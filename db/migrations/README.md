# Database Migrations

Migration memakai nama berurutan dengan pasangan file:

```text
000001_foundation.up.sql
000001_foundation.down.sql
000002_catalog_inventory.up.sql
000002_catalog_inventory.down.sql
```

Saat startup, API meng-embed dan menjalankan hanya `*.up.sql` yang belum ada di tabel `schema_migrations`. PostgreSQL advisory lock mencegah dua replica menjalankan migration bersamaan. Satu file forward migration dan pencatatan versinya dijalankan dalam transaction yang sama.

File `*.down.sql` tidak pernah dijalankan otomatis. Rollback merupakan tindakan operator yang disengaja dan harus dilakukan setelah backup serta pemeriksaan target database.

Folder [`../schema/`](../schema/) tetap merupakan snapshot/reference schema dan tidak dieksekusi oleh migration runner.

