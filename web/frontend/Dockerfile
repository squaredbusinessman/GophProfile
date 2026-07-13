FROM node:24-bookworm-slim AS build

WORKDIR /src

COPY package.json package-lock.json ./
RUN npm ci

COPY . .
RUN npm run build

FROM nginx:1.29-alpine

ENV API_BACKEND=http://server:8080
ENV NGINX_RESOLVER=127.0.0.11
ENV NGINX_ENVSUBST_FILTER=API_BACKEND|NGINX_RESOLVER

COPY nginx.conf /etc/nginx/templates/default.conf.template
COPY --from=build /static /usr/share/nginx/html/web

EXPOSE 80
