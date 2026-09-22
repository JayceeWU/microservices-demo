FROM node:22-alpine AS build
WORKDIR /workspace
COPY package.json package-lock.json ./
COPY apps/teacher-web/package.json apps/teacher-web/package.json
COPY packages/api-client/package.json packages/api-client/package.json
COPY packages/auth/package.json packages/auth/package.json
COPY packages/telemetry/package.json packages/telemetry/package.json
COPY packages/ui/package.json packages/ui/package.json
RUN --mount=type=cache,id=dancehub-npm,target=/root/.npm npm ci --workspace teacher-web --include-workspace-root
COPY apps/teacher-web apps/teacher-web
COPY packages packages
RUN npm run build --workspace teacher-web

FROM nginx:1.27-alpine
COPY --from=build /workspace/apps/teacher-web/dist/teacher-web/browser /usr/share/nginx/html
COPY apps/nginx.conf /etc/nginx/nginx.conf
COPY apps/nginx-spa.conf /etc/nginx/conf.d/default.conf
COPY apps/runtime-config.js.template /etc/nginx/templates/runtime-config.js.template
ENV NGINX_ENVSUBST_OUTPUT_DIR=/tmp
USER 101:101
EXPOSE 8080
