# UI dependencies

Groovebox serves these locally so unavailable external networks cannot remove
its layout, text colors or navigation icons. No runtime CDN requests are needed.

- normalize.css 8.0.1: https://unpkg.com/normalize.css@8.0.1/normalize.css (MIT)
- Blueprint core 5.19.1: https://unpkg.com/@blueprintjs/core@5.19.1/lib/css/blueprint.css (Apache-2.0)
- Blueprint icons 5.23.0: https://unpkg.com/@blueprintjs/icons@5.23.0/lib/css/blueprint-icons.css (Apache-2.0)

These are the versions the former `@5` CDN URLs resolved to on 2026-10-24.
The icon stylesheet is restricted to the included WOFF2 files; unused legacy
font formats are omitted. Upstream license texts are alongside the assets.
