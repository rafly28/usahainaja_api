# Backend

Backend vertical slice pertama UsahainAja menggunakan Go 1.22, `chi`, `pgxpool`, dan PostgreSQL. Scope saat ini sengaja dibatasi pada autentikasi, business context, produk, serta opening stock.

## Menjalankan secara lokal

Prasyarat: Go 1.22 dan PostgreSQL dengan extension `pgcrypto` tersedia.

```bash
cp .env.example .env
set -a
. ./.env
set +a
go run ./cmd/api
```

`DATABASE_URL` wajib diisi. Binary akan mengambil PostgreSQL advisory lock lalu menjalankan embedded forward migrations yang belum tercatat sebelum membuka port HTTP. Default address adalah `:8080`.

Environment yang didukung:

| Variable | Default | Keterangan |
|---|---:|---|
| `DATABASE_URL` | wajib | PostgreSQL connection string |
| `HTTP_ADDR` | `:8080` | Address API |
| `COOKIE_NAME` | `usahainaja_session` | Nama cookie session |
| `COOKIE_SECURE` | `false` | Wajib `true` pada HTTPS production |
| `SESSION_TTL` | `168h` | Masa berlaku server-side session |
| `BCRYPT_COST` | `12` | Cost bcrypt, rentang 4–31 |
| `SHUTDOWN_TIMEOUT` | `10s` | Batas graceful shutdown |

## Menjalankan lewat Screen

Untuk server development yang berjalan lama, gunakan session backend setelah file .env tersedia:

    screen -S backend -dm bash -lc 'set -a && . ./.env && set +a && exec go run ./cmd/api'
    screen -r backend

Detach dengan Ctrl-a lalu d. Cek session memakai screen -ls dan hentikan hanya session sendiri dengan screen -S backend -X quit.

## Endpoint milestone

```text
GET  /api/health
GET  /api/auth/csrf
POST /api/auth/register
POST /api/auth/login
POST /api/auth/logout
GET  /api/auth/me
POST /api/auth/switch-business
GET  /api/businesses
POST /api/businesses
GET  /api/businesses/current
GET  /api/products
POST /api/products
POST /api/inventory/opening-stock
GET  /api/inventory/products
```

Register menerima `{ "name", "email", "password" }`; login menerima `{ "email", "password" }`. Keduanya membuat session ID baru, menyetel cookie `HttpOnly; SameSite=Lax`, dan mengembalikan `csrf_token`. Cookie lama pada browser tersebut direvoke ketika autentikasi berhasil.

Semua mutation setelah login harus mengirim header:

```http
X-CSRF-Token: <token dari register/login atau GET /api/auth/csrf>
```

Contoh body utama:

```json
{
  "name": "Toko Buah Segar",
  "business_type": "RETAIL",
  "timezone": "Asia/Jakarta",
  "currency": "IDR"
}
```

Create business otomatis membuat lokasi `LOC-DEFAULT`, role `OWNER/ADMIN/CASHIER/STAFF/VIEWER`, satuan `PCS/KG/GRAM/LITER/ML`, number sequences, membership owner, audit log, dan menjadikannya business aktif pada session.

```json
{
  "name": "Apel Fuji",
  "sku": "APL-FJ",
  "base_unit_symbol": "KG",
  "default_purchase_price": "30000",
  "default_selling_price": "38000",
  "min_stock": "5",
  "is_stock_tracked": true
}
```

Harga dan kuantitas menerima JSON string atau number, tetapi response selalu string agar presisi decimal tidak hilang.

```json
{
  "product_code": "PRD-000001",
  "location_code": "LOC-DEFAULT",
  "quantity": "20.5",
  "reason": "Saldo awal pilot"
}
```

Opening stock hanya boleh dicatat satu kali untuk setiap pasangan produk dan lokasi. Operasi membuat adjustment, item, stock movement, inventory snapshot, dan audit log dalam satu transaction. `stock_movements` tetap source of truth; `product_inventory` hanyalah snapshot yang dapat dibangun ulang.

Product dan opening-stock mutation saat ini dibatasi untuk `OWNER` dan `ADMIN`. Seluruh query produk, lokasi, unit, movement, serta inventory menggunakan `business_id` dari authenticated session—tidak pernah dari request body.

## Response

Sukses:

```json
{
  "success": true,
  "data": {}
}
```

Gagal:

```json
{
  "success": false,
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Periksa kembali data yang dikirim.",
    "fields": {
      "name": "Nama wajib diisi."
    },
    "request_id": "..."
  }
}
```

## Verifikasi

Unit tests tidak membutuhkan PostgreSQL:

```bash
go test ./...
go vet ./...
go build -o /tmp/usahainaja-api ./cmd/api
```

Tests mencakup normalisasi identitas dan bcrypt, rotasi session, presisi decimal, domain conflict opening stock, CSRF, canonical collection routes tanpa trailing slash, JSON content type, dan role guard.

## Struktur

```text
cmd/api/                  process startup dan graceful shutdown
db/migrations/            migration runner + ordered up/down SQL
db/schema/                snapshot schema referensi (tidak dieksekusi)
internal/app/             validation dan application service
internal/httpapi/         route, middleware, cookie, envelope
internal/postgres/        tenant-scoped repository dan transaction
internal/config/          environment configuration
```
