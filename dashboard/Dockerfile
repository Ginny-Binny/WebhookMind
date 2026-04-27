# Build the SolidJS dashboard with Vite, then serve via nginx.
# Build context must be the repo root so we can COPY dashboard/...

FROM node:20-alpine AS builder

WORKDIR /src

COPY dashboard/package.json dashboard/package-lock.json ./
RUN npm ci

COPY dashboard/ ./
RUN npm run build


FROM nginx:1.27-alpine

COPY --from=builder /src/dist /usr/share/nginx/html
COPY dashboard/nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80
