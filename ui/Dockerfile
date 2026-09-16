# Multi-stage build for the kcert-checker Next.js UI.
# Stage 1: install dependencies and build the Next.js app.
# Stage 2: run the production server as a non-root user.

FROM node:22-alpine AS builder
WORKDIR /app

# Install dependencies first (separate layer — cached unless package.json changes).
COPY package.json package-lock.json* ./
RUN npm ci

# Copy source and build. NEXT_PUBLIC_API_URL is baked in at build time if set;
# if not set, next.config.ts falls back to http://localhost:8080 for dev use.
COPY . .
RUN npm run build

# --------------------------------------------------------------------------
FROM node:22-alpine AS runner
WORKDIR /app

# Create a non-root user matching the securityContext.runAsUser in values.yaml.
RUN addgroup --system --gid 1001 nodejs && \
    adduser  --system --uid 1001 nextjs

# Copy only the standalone output produced by `output: "standalone"` in next.config.ts.
# This is the minimal production artifact — no node_modules needed at runtime.
COPY --from=builder --chown=nextjs:nodejs /app/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /app/.next/static ./.next/static
COPY --from=builder --chown=nextjs:nodejs /app/public ./public

USER nextjs
EXPOSE 3000
ENV PORT=3000
ENV HOSTNAME="0.0.0.0"

CMD ["node", "server.js"]
