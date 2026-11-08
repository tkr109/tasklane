FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/tasklane .
RUN mkdir /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/tasklane /tasklane
# A fresh volume mounted at /data takes this owner, so the nonroot user can write.
COPY --from=build --chown=65532:65532 /out/data /data
ENTRYPOINT ["/tasklane"]
CMD ["api"]
