## Task

- Task ID:
- Contract/ADR yang digunakan:
- Branch target: main

## Perubahan dan risiko

- [ ] Tidak mengubah migration lama; migration baru sudah direview.
- [ ] Tidak menambah secret, file environment aktif, cache, atau artefak build.
- [ ] Endpoint/payload dan dokumentasi kontrak sudah diperbarui bila diperlukan.
- [ ] Risiko serta rollback dicatat.

## Verifikasi

- [ ] `go test ./...`
- [ ] `go vet ./...`
- [ ] `test -z "$(gofmt -l .)"`
- [ ] Test PostgreSQL/integration relevan dijalankan atau alasan belum tersedia dicatat.

## Handoff

- [ ] `docs/task_updates` telah diperbarui dengan branch/PR, SHA, hasil check, Screen, dan tindak lanjut.
