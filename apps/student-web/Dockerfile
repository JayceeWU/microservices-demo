FROM node:22-alpine AS deps
WORKDIR /workspace
COPY package.json package-lock.json ./
COPY apps/student-web/package.json apps/student-web/package.json
COPY packages/api-client/package.json packages/api-client/package.json
COPY packages/telemetry/package.json packages/telemetry/package.json
COPY packages/ui/package.json packages/ui/package.json
RUN --mount=type=cache,id=dancehub-npm,target=/root/.npm npm ci --workspace student-web --include-workspace-root

FROM node:22-alpine AS build
WORKDIR /workspace
COPY --from=deps /workspace/node_modules ./node_modules
COPY package.json package-lock.json ./
COPY apps/student-web apps/student-web
COPY packages packages
ENV NEXT_TELEMETRY_DISABLED=1
RUN npm run build --workspace student-web

FROM node:22-alpine AS runner
WORKDIR /app
ENV NODE_ENV=production NEXT_TELEMETRY_DISABLED=1 PORT=3000 HOSTNAME=0.0.0.0
COPY --from=build /workspace/apps/student-web/.next/standalone ./
COPY --from=build /workspace/apps/student-web/.next/static ./apps/student-web/.next/static
USER 1000:1000
EXPOSE 3000
CMD ["node", "apps/student-web/server.js"]
