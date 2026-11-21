# Desain Musehood Go

Musehood Go memindahkan Musehood milik Arman ke satu binary Go dengan frontend SvelteKit statis. API, data versi 1, urutan kursi, perilaku chat, antrean file pekerja, teks UI Indonesia, CSS, dan avatar mengikuti repo Musehood Node.

- Backend `internal/musehood` memakai standard library. Mutex melindungi state. Panggilan HTTP model berjalan di luar mutex. Setiap job maju satu langkah pada satu waktu. Tick membaca antrean dan dapat melanjutkan job dari `data.json` setelah restart.
- State ditulis atomik dengan izin privat. Root runtime berisi `data.json`, `queue/`, `avatars/`, dan `images/`. URL media tetap `/avatars/` dan `/images/`. Header Host dan Origin diperiksa di Go, dengan listener IPv4 loopback dan `TRUSTED_HOSTS` untuk proxy lokal.
- Svelte 5 merender halaman dan panel secara reaktif. Polling memuat pesan, status, dan avatar terkini. Form menyimpan draftnya selama polling. Adapter-static membangun fallback `index.html` dan `embed` memasukkannya ke binary.
- Pengujian memakai HTTP lokal, fixture gambar, respons file pekerja, dan direktori sementara. Smoke test menyalin binary ke direktori baru dan memulai proses dari sana. Browser Chromium menguji alur UI dan layout.
