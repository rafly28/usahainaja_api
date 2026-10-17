# Aturan Kontributor AI — Backend

Instruksi ini berlaku untuk semua pekerjaan di repository backend.

## Git dan ownership task

1. Satu AI hanya mengerjakan satu task aktif. Jangan mengubah file yang sedang dikerjakan AI lain.
2. Sebelum mengubah file, cek branch dan working tree dengan git status --short --branch.
3. Setiap task harus berada pada branch baru dari origin/main dengan format task/TXX-ringkasan atau feat/ringkasan. Tidak ada commit atau push langsung ke main.
4. Bila terdapat WIP yang memang milik task tersebut di main, buat branch baru terlebih dahulu dengan git switch -c task/TXX-ringkasan. Perintah ini mempertahankan perubahan lokal. Jangan memakai reset, checkout, clean, atau membuang stash untuk memindahkan WIP tanpa persetujuan maintainer.
5. Jika WIP tidak jelas pemiliknya, berhenti dan laporkan file tersebut; jangan menimpa, memformat massal, atau memasukkannya ke commit task sendiri. Bila working tree sedang dipakai sesi lain, buat worktree terpisah dari `origin/main` untuk pekerjaanmu: `git worktree add /tmp/uja-<task> -b task/TXX-ringkasan origin/main`.
6. Sebelum merge, sinkronkan branch task dengan origin/main, selesaikan konflik di branch task, dan buka pull request. Main hanya menerima PR dengan required checks hijau.

## Aturan implementasi

1. Ikuti docs/transaction_contract_v1.md, ADR accepted, dan docs/tasks/TXX terkait sebagai kontrak sebelum mengubah endpoint, schema, atau state transaction. Jika clone backend berdiri sendiri tanpa folder docs, minta atau tautkan dokumen koordinasi tersebut sebelum melanjutkan; jangan menebak kontraknya.
2. Migration lama immutable. Selalu buat migration maju; jangan mengedit migration yang pernah dipakai.
3. Mutasi uang, stok, dan status harus atomik, tenant-aware, teraudit, serta idempotent jika kontrak memerlukannya.
4. Jangan mengakses database langsung dari adapter AI, WhatsApp, atau integrasi. Semua melalui Core API.
5. Jalankan proses backend jangka panjang melalui GNU Screen session bernama backend. Cek screen -ls terlebih dahulu dan jangan menghentikan session milik task lain.

## Batas ukuran file

Ketentuan ini bersifat praktis dan bertahap. Tidak diperbolehkan menambah file baru yang melebihi 500 baris. File lama yang sudah melebihi batas direfaktor ketika disentuh task, dan sisanya dicatat sebagai utang teknis.

1. Satu file source code maksimal 500 baris.
2. File yang sudah melebihi 500 baris wajib direfaktor ketika disentuh task, secara bertahap, mengikuti domain dan tanggung jawab. Pemecahan bukan sekadar memotong baris; helper yang dipakai beberapa modul harus diekstrak agar reusable.
3. Refactor wajib mempertahankan perilaku: kontrak API, bentuk payload, error code, dan hasil perhitungan tidak boleh berubah hanya demi mengurangi ukuran file.
4. Utang teknis yang tidak bisa diselesaikan dalam scope task dicatat di `docs/task_updates/` beserta pemilik task berikutnya. Refactor struktural backend adalah milik T16.
5. Pengecualian: file generated atau vendored, dokumen schema referensi di `db/schema/`, dan file SQL migration. Pengecualian tidak berlaku untuk `.go`.

## Infrastruktur lokal

1. **Gunakan container yang sudah ada di host: `itams_db` (PostgreSQL) dan `itams_redis` (Redis).** Jangan membuat container, volume, atau database PostgreSQL baru untuk development, test manual, atau QC.
2. Kredensial development yang dipakai: database `usahainaja`, user `usahainaja`, port `5432` dari host, Redis `127.0.0.1:6379`. Sesuaikan lewat Makefile, bukan dengan membuat infra baru.
3. Jalankan seluruh environment development dengan `make dev` dari root workspace. Perintah itu menyalakan `itams_db`/`itams_redis` bila mati, lalu menjalankan backend dan frontend lewat GNU Screen tanpa build image.
4. `make dev-compose` hanya untuk QC Container penuh saat image benar-benar perlu dibangun. Jangan jadikan mode development harian.
5. Migration harus berhasil pada database yang **sudah berisi data**, bukan hanya database kosong. Hindari pola `ADD COLUMN ... NOT NULL DEFAULT x CHECK (...)` pada tabel berisi baris; gunakan urutan tambah kolomnullable, backfill, baru `SET NOT NULL` dan constraint. Verifikasi dengan `tests/integration` atau smoke test terhadap database lama, bukan selalu volume baru.
6. Jangan menghapus atau me-reset `itams_db`/`itams_redis` tanpa persetujuan maintainer karena keduanya dipakai bersama proyek lain di mesin ini. `make dev-infra-stop` memang meminta konfirmasi.

## Sebelum handoff

1. Jalankan test, vet, dan gofmt check yang relevan.
2. Perbarui kontrak API atau addendum bila payload/endpoint berubah.
3. Buat docs/task_updates/TXX-YYYY-MM-DD-ringkasan.md berisi branch/PR, file, verifikasi, session Screen, risiko, serta tindak lanjut. Jangan menulis secret.
4. Commit hanya source, migration, test, dan dokumentasi yang relevan. Jangan commit file env, credential, cache, coverage sementara, atau artefak build.
