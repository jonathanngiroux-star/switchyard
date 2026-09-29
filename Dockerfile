FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/switchyard ./cmd/switchyard

FROM alpine:3.20
COPY --from=build /out/switchyard /usr/local/bin/switchyard
EXPOSE 8080
ENTRYPOINT ["switchyard"]
CMD ["serve", "--addr", ":8080"]
