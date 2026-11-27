# Musehood Go — image multi-stage.
#
# CATATAN JUJUR soal sandbox: tool run_code / run_shell / run_build /
# run_tests / run_server menjalankan kode di dalam bubblewrap (bwrap)
# dengan user namespace. Di dalam container, bwrap butuh hak istimewa
# tambahan — jalankan container dengan --privileged (atau
# --cap-add SYS_ADMIN dan seccomp unconfined) agar sandbox bekerja.
# Tanpa itu, tool-tool kode jatuh ke mode terbatas: eksekusi gagal
# dengan pesan error yang jelas, tetapi fitur chat lainnya tetap jalan.

# --- Tahap 1: build frontend (SvelteKit) ---
FROM node:22-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

# --- Tahap 2: build binary Go ---
FROM golang:1.27-alpine AS backend
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /src/frontend/build ./frontend/build
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/musehood-go .

# --- Tahap 3: runtime ---
FROM alpine:3.20
RUN apk add --no-cache \
      git \
      nodejs \
      python3 \
      tesseract-ocr \
      espeak-ng \
      poppler-utils \
      bubblewrap \
      ca-certificates \
    && addgroup -S musehood \
    && adduser -S musehood -G musehood
COPY --from=backend /out/musehood-go /usr/local/bin/musehood-go
RUN mkdir -p /data && chown musehood:musehood /data
USER musehood
EXPOSE 3001
VOLUME ["/data"]
ENTRYPOINT ["/usr/local/bin/musehood-go", "-data-dir", "/data", "-host", "0.0.0.0", "-port", "3001"]
