# Branding

Edit **`branding/branding.yaml`** only. Then:

```bash
make sync-branding
```

That regenerates:

| Output | Used by |
|--------|---------|
| `services/web-service/src/styles/branding.generated.css` | App CSS variables (`globals.css` imports it) |
| `services/web-service/src/lib/branding.generated.ts` | Optional TS access (`brand.name`, `brand.email.*`) |
| `services/web-service/public/sift-logo.svg` | Favicon / static mark (stroke from `logo.file_stroke`) |
| `services/auth-service/lib/sift-logo-email.png` | Password-reset email logo (CID attachment; Gmail strips SVG) |
| `services/web-service/public/sift-logo-email.png` | Same PNG hosted publicly as fallback |
| `services/auth-service/lib/branding.generated.json` | Password-reset HTML email colors |

Web UI logos use `currentColor` + `text-foreground`. Emails cannot use SVG in Gmail/Outlook, so the logo is a CID-attached PNG tinted with `email.mark`.

`make sync-web` / `make sync-auth` automatically run `sync-branding` first.
