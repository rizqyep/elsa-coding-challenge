# syntax=docker/dockerfile:1
# Builds the client and serves it from nginx, which also proxies /api and /ws. Build context: the repo root.

FROM node:22-alpine AS build
WORKDIR /client
COPY client/package.json client/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY client/ ./
RUN npm run build

FROM nginx:1.28-alpine
COPY deploy/nginx/nginx.conf /etc/nginx/nginx.conf
COPY --from=build /client/dist /usr/share/nginx/html
