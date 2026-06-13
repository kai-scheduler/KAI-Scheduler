# Design Presentations

Self-contained HTML presentations of KAI Scheduler design docs, published to GitHub Pages at:

**https://kai-scheduler.github.io/KAI-Scheduler/presentations/**

## Adding a presentation

1. Drop a self-contained `*.html` file into this folder. Any CSS it needs must be either inlined or kept as a local `*.css` file in this same folder (no external build step).
2. Give the file a meaningful `<title>` — it becomes the link text on the index page.
3. Open a PR. On merge to `main`, the [`deploy-design-presentations`](../../.github/workflows/deploy-design-presentations.yaml) workflow regenerates the index and publishes the site.

## Notes

- **Do not commit an `index.html`** — it is generated at deploy time by [`build-index.sh`](build-index.sh) from the `<title>` of each presentation. A hand-written one would be overwritten.
- This `README.md` and `build-index.sh` are not published — only `*.html` and `*.css` files reach the site.
- To preview the generated index locally: `bash docs/presentations/build-index.sh /tmp/pres-site && open /tmp/pres-site/index.html`.
