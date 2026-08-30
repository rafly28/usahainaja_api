# Aturan Kontributor AI — Backend

1. Satu AI hanya memegang satu task aktif. Cek `git status --short --branch` sebelum mengubah file.
2. Setiap task memakai branch `task/TXX-ringkasan` atau `feat/ringkasan`; jangan commit atau push langsung ke `main`.
3. Jika WIP memang milik task, buat branch dengan `git switch -c task/TXX-ringkasan` agar WIP tetap terbawa. Jangan memakai reset, checkout, clean, atau menghapus stash tanpa persetujuan maintainer.
4. Jika WIP tidak jelas pemiliknya, jangan menyentuh atau memasukkannya ke commit task.
5. Ikuti Transaction Contract, ADR accepted, dan task terkait sebelum mengubah schema, endpoint, atau state. Bila clone mandiri tidak memiliki docs, minta dokumen koordinasi; jangan menebak kontrak.
6. Migration lama immutable; gunakan migration maju. Mutasi uang, stok, dan status harus atomik, tenant-aware, teraudit, serta idempotent bila kontrak mewajibkan.
7. Proses backend jangka panjang wajib memakai GNU Screen session `backend`. Jangan menghentikan session milik task lain.
8. Sebelum handoff, jalankan test, vet, dan gofmt check; perbarui kontrak bila perlu; catat branch/PR, SHA, verifikasi, Screen, risiko, dan tindak lanjut di handoff tanpa secret.
