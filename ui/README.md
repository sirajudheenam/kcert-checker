# kcert UI

Next.js 16 dashboard for the kcert-checker Go backend.

## Development

```bash
# Point at your local Go backend (default: http://localhost:8080)
cp .env.local.example .env.local

npm install
npm run dev     # http://localhost:3000
```

The dev server proxies `/api/*` to the backend URL configured in `NEXT_PUBLIC_API_URL`.

## Production build

```bash
npm run build
npm start
```

## Architecture

- `app/page.tsx` — server component, SSR-fetches initial cert list
- `app/Dashboard.tsx` — client component, handles sort/filter/auto-refresh
- `app/api-client.ts` — fetch wrapper pointing at Go `/api/certificates`
- `app/types.ts` — TypeScript types matching Go JSON output
- `app/status.ts` — status badge classes and day formatting helpers

## To deploy with the Next.js UI enabled:

```bash
docker build -t kcert-checker-ui:local ./ui
kind load docker-image kcert-checker-ui:local
helm upgrade kcert-checker ./helm/kcert-checker \
  --set ui.enabled=true \
  --set ui.image.repository=kcert-checker-ui \
  --set ui.image.tag=local \
  --set ui.image.pullPolicy=Never
```