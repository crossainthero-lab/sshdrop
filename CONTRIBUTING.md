# Contributing

SSHDrop is a Go terminal application. Keep changes small, tested, and scoped to
the package that owns the behavior.

Before submitting changes, run:

```bash
go fmt ./...
go vet ./...
go test ./...
go build ./cmd/sshdrop
```

Do not add code paths that disable SSH host-key verification or store plaintext
passwords. Integration tests must use isolated temporary keys and test servers.
