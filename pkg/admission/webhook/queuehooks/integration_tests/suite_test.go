// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package integration_tests

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/kai-scheduler/KAI-scheduler/pkg/admission/webhook/queuehooks"
	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
)

const (
	webhookPath = "/validate-scheduling-run-ai-v2-queue"
	timeout     = time.Second * 20
	interval    = time.Millisecond * 250
)

var (
	testEnv    *envtest.Environment
	cfg        *rest.Config
	k8sClient  client.Client        // creates/updates queues, records admission warnings
	readClient client.Client        // used by the validator to read parent/children
	ctx        context.Context
	cancel     context.CancelFunc
	mgrDone    chan struct{}

	// currentMode is flipped per spec; the registered validator delegates to a
	// fresh queuehooks validator built with this mode on every admission call.
	currentMode = queuehooks.OverSubscriptionModeNone

	warnings = &recordingWarnings{}
)

// recordingWarnings captures admission warning headers surfaced to the client.
type recordingWarnings struct {
	mu   sync.Mutex
	msgs []string
}

func (r *recordingWarnings) HandleWarningHeader(code int, agent, msg string) {
	if msg == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, msg)
}

func (r *recordingWarnings) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = nil
}

func (r *recordingWarnings) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.msgs...)
}

// switchableValidator implements queuehooks.QueueValidator and delegates to a
// validator built with the currently selected enforcement mode.
type switchableValidator struct {
	reader client.Client
}

func (s *switchableValidator) ValidateCreate(ctx context.Context, q *v2.Queue) (admission.Warnings, error) {
	return queuehooks.NewQueueValidator(s.reader, currentMode).ValidateCreate(ctx, q)
}

func (s *switchableValidator) ValidateUpdate(ctx context.Context, oldQ, newQ *v2.Queue) (admission.Warnings, error) {
	return queuehooks.NewQueueValidator(s.reader, currentMode).ValidateUpdate(ctx, oldQ, newQ)
}

func (s *switchableValidator) ValidateDelete(ctx context.Context, q *v2.Queue) (admission.Warnings, error) {
	return queuehooks.NewQueueValidator(s.reader, currentMode).ValidateDelete(ctx, q)
}

func TestQueueValidatorIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Queue Validator Integration Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	ctx, cancel = context.WithCancel(context.Background())

	failPolicy := admissionregistrationv1.Fail
	sideEffects := admissionregistrationv1.SideEffectClassNone
	scope := admissionregistrationv1.ClusterScope
	matchPolicy := admissionregistrationv1.Equivalent

	webhookConfig := &admissionregistrationv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "kai-queue-validation-integration"},
		Webhooks: []admissionregistrationv1.ValidatingWebhook{
			{
				Name:                    "queue-validation.kai.scheduler",
				AdmissionReviewVersions: []string{"v1"},
				SideEffects:             &sideEffects,
				FailurePolicy:           &failPolicy,
				MatchPolicy:             &matchPolicy,
				ClientConfig: admissionregistrationv1.WebhookClientConfig{
					Service: &admissionregistrationv1.ServiceReference{
						Name:      "kai-queue-validation",
						Namespace: "default",
						Path:      ptr.To(webhookPath),
					},
				},
				Rules: []admissionregistrationv1.RuleWithOperations{
					{
						Operations: []admissionregistrationv1.OperationType{
							admissionregistrationv1.Create,
							admissionregistrationv1.Update,
						},
						Rule: admissionregistrationv1.Rule{
							APIGroups:   []string{"scheduling.run.ai"},
							APIVersions: []string{"v2"},
							Resources:   []string{"queues"},
							Scope:       &scope,
						},
					},
				},
			},
		},
	}

	By("bootstrapping test environment with a validating webhook")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "..", "..", "deployments", "kai-scheduler", "crds"),
		},
		ErrorIfCRDPathMissing: true,
		WebhookInstallOptions: envtest.WebhookInstallOptions{
			ValidatingWebhooks: []*admissionregistrationv1.ValidatingWebhookConfiguration{webhookConfig},
		},
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	Expect(v2.AddToScheme(scheme.Scheme)).To(Succeed())

	readClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())

	// Client that records admission warnings surfaced by the API server.
	warnCfg := rest.CopyConfig(cfg)
	warnCfg.WarningHandler = warnings
	k8sClient, err = client.New(warnCfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())

	webhookOpts := testEnv.WebhookInstallOptions
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:  scheme.Scheme,
		Metrics: metricsserver.Options{BindAddress: "0"},
		WebhookServer: webhook.NewServer(webhook.Options{
			Host:    webhookOpts.LocalServingHost,
			Port:    webhookOpts.LocalServingPort,
			CertDir: webhookOpts.LocalServingCertDir,
		}),
	})
	Expect(err).NotTo(HaveOccurred())

	Expect(ctrl.NewWebhookManagedBy(mgr, &v2.Queue{}).
		WithValidator(&switchableValidator{reader: readClient}).
		Complete()).To(Succeed())

	mgrDone = make(chan struct{})
	go func() {
		defer GinkgoRecover()
		defer close(mgrDone)
		Expect(mgr.Start(ctx)).To(Succeed())
	}()

	By("waiting for the webhook server to become ready")
	Eventually(func() error {
		return mgr.GetWebhookServer().StartedChecker()(nil)
	}, timeout, interval).Should(Succeed())
})

var _ = AfterSuite(func() {
	cancel()
	if mgrDone != nil {
		<-mgrDone
	}
	if testEnv != nil {
		Expect(testEnv.Stop()).To(Succeed())
	}
})
