<script lang="ts">
  import { NOTIFY_KEY } from './chat.svelte';
  import { i18n, setLocale, t, type Locale } from './i18n.svelte';
  import type { ReviewLevel, SeatEdit } from './types';

  let notifyOn = $state(typeof localStorage !== 'undefined' && localStorage.getItem(NOTIFY_KEY) === '1');

  async function toggleNotify(event: Event) {
    const input = event.currentTarget as HTMLInputElement;
    if (input.checked) {
      if (typeof Notification === 'undefined') { input.checked = false; return; }
      const permission = await Notification.requestPermission();
      if (permission !== 'granted') { input.checked = false; notifyOn = false; return; }
      localStorage.setItem(NOTIFY_KEY, '1');
      notifyOn = true;
    } else {
      localStorage.setItem(NOTIFY_KEY, '0');
      notifyOn = false;
    }
  }

  let {
    roundRobin = $bindable(true), reviewLevel = $bindable<ReviewLevel>('Sedang'),
    integratorSeat = $bindable('MUSE'), demoFallback = $bindable(false), toolsEnabled = $bindable(true), codeToolEnabled = $bindable(true), streamingEnabled = $bindable(true),
    theme = $bindable('light'), fontSize = $bindable('M'), seats,
  }: {
    roundRobin?: boolean;
    reviewLevel?: ReviewLevel;
    integratorSeat?: string;
    demoFallback?: boolean;
    toolsEnabled?: boolean;
    codeToolEnabled?: boolean;
    streamingEnabled?: boolean;
    theme?: string;
    fontSize?: string;
    seats: SeatEdit[];
  } = $props();

  const levels: ReviewLevel[] = ['Longgar', 'Santai', 'Sedang', 'Ketat'];
</script>

<section class="settings-section">
  <h3>Cara berdiskusi</h3>
  <label class="toggle-row"><span><strong>Diskusi Bergilir</strong><small>Semua kursi membalas bergiliran. Jika mati, hanya Muse tanpa @mention.</small></span><input id="roundRobin" type="checkbox" role="switch" bind:checked={roundRobin}></label>
  <label>Level Review<select id="reviewLevel" bind:value={reviewLevel}>{#each levels as level (level)}<option value={level}>{level}</option>{/each}</select></label>
  <p class="field-hint">Mengatur ketajaman pemeriksaan kursi Peninjau.</p>
  <label>Kursi integrator<select id="integratorSeat" bind:value={integratorSeat}>{#each seats as item (item.code)}<option value={item.code}>{item.name} ({item.code})</option>{/each}</select></label>
  <p class="field-hint">Kursi yang menutup pipeline "Tugas:" (Hasil Akhir - Integrasi) dan menjawab "Tanya Progres". Bawaan: Muse (MUSE).</p>
  <label class="toggle-row"><span><strong>Fallback demo</strong><small>Simulasi berlabel DEMO jika chat gateway gagal. Tidak berlaku untuk Muse atau gambar.</small></span><input id="demoFallback" type="checkbox" role="switch" bind:checked={demoFallback}></label>
  <label class="toggle-row"><span><strong>Tool untuk kursi gateway</strong><small>Kursi gateway dapat memakai tool saat menjawab: pencarian web, mengambil halaman web, membaca/menulis/mengubah/mengunduh file di folder kerja (sandbox), membuat gambar, menyimpan catatan ke memori kursi, dan waktu sekarang. Menghapus file menunggu persetujuanmu. Maksimal 3 putaran tool per balasan.</small></span><input id="toolsEnabled" type="checkbox" role="switch" bind:checked={toolsEnabled}></label>
  <label class="toggle-row"><span><strong>Notifikasi browser</strong><small>Tampilkan notifikasi saat kursi selesai membalas dan tab aplikasi sedang tidak aktif. Izin notifikasi diminta dari browser perangkat ini.</small></span><input id="notifyEnabled" type="checkbox" role="switch" checked={notifyOn} onchange={toggleNotify}></label>
  <label class="toggle-row"><span><strong>Streaming balasan</strong><small>Teks balasan kursi gateway muncul bertahap selagi model menjawab. Bila gateway tidak mendukung streaming, aplikasi otomatis memakai cara biasa.</small></span><input id="streamingEnabled" type="checkbox" role="switch" bind:checked={streamingEnabled}></label>
  <label class="toggle-row"><span><strong>Tool coding (run_code & run_shell)</strong><small>Kursi gateway dapat menjalankan kode Python/JavaScript dan perintah shell di sandbox terisolasi (user nobody, tanpa jaringan, maks 10 detik). Kode berbahaya dan semua perintah shell menunggu persetujuanmu di banner chat sebelum dijalankan.</small></span><input id="codeToolEnabled" type="checkbox" role="switch" bind:checked={codeToolEnabled}></label>
  <label>{t('theme')}<select id="themeSelect" bind:value={theme}><option value="light">{t('themeLight')}</option><option value="dark">{t('themeDark')}</option><option value="auto">{t('themeAuto')}</option></select></label>
  <label>{t('fontSize')}<select id="fontSizeSelect" bind:value={fontSize}><option value="S">{t('fontSmall')}</option><option value="M">{t('fontMedium')}</option><option value="L">{t('fontLarge')}</option></select></label>
  <label>{t('language')}<select id="localeSelect" value={i18n.locale} onchange={(e) => setLocale((e.currentTarget as HTMLSelectElement).value as Locale)}><option value="id">Bahasa Indonesia</option><option value="en">English</option></select></label>
</section>
