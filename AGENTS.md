# Aturan Kontributor AI — Backend

Instruksi ini berlaku untuk semua pekerjaan di repository backend.

## Git dan ownership task

1. Satu AI hanya mengerjakan satu task aktif. Jangan mengubah file yang sedang dikerjakan AI lain.
2. Sebelum mengubah file, cek branch dan working tree dengan git status --short --branch.
3. Setiap task harus berada pada branch baru dari origin/main dengan format task/TXX-ringkasan atau feat/ringkasan. Tidak ada commit atau push langsung ke main.
4. Bila terdapat WIP yang memang milik task tersebut di main, buat branch baru terlebih dahulu dengan git switch -c task/TXX-ringkasan. Perintah ini mempertahankan perubahan lokal. Jangan memakai reset, checkout, clean, atau membuang stash untuk memindahkan WIP tanpa persetujuan maintainer.
5. Jika WIP tidak jelas pemiliknya, berhenti dan laporkan file tersebut; jangan menimpa, memformat massal, atau memasukkannya ke commit task sendiri.
6. Sebelum merge, sinkronkan branch task dengan origin/main, selesaikan konflik di branch task, dan buka pull request. Main hanya menerima PR dengan required checks hijau.

## Aturan implementasi

1. Ikuti docs/transaction_contract_v1.md, ADR accepted, dan docs/tasks/TXX terkait sebagai kontrak sebelum mengubah endpoint, schema, atau state transaction. Jika clone backend berdiri sendiri tanpa folder docs, minta atau tautkan dokumen koordinasi tersebut sebelum melanjutkan; jangan menebak kontraknya.
2. Migration lama immutable. Selalu buat migration maju; jangan mengedit migration yang pernah dipakai.
3. Mutasi uang, stok, dan status harus atomik, tenant-aware, teraudit, serta idempotent jika kontrak memerlukannya.
4. Jangan mengakses database langsung dari adapter AI, WhatsApp, atau integrasi. Semua melalui Core API.
5. Jalankan proses backend jangka panjang melalui GNU Screen session bernama backend. Cek screen -ls terlebih dahulu dan jangan menghentikan session milik task lain.

## Sebelum handoff

1. Jalankan test, vet, dan gofmt check yang relevan.
2. Perbarui kontrak API atau addendum bila payload/endpoint berubah.
3. Buat docs/task_updates/TXX-YYYY-MM-DD-ringkasan.md berisi branch/PR, file, verifikasi, session Screen, risiko, serta tindak lanjut. Jangan menulis secret.
4. Commit hanya source, migration, test, dan dokumentasi yang relevan. Jangan commit file env, credential, cache, coverage sementara, atau artefak build.
