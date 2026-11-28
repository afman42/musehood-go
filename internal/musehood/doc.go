// Package musehood adalah backend Musehood Go: server HTTP multi-kursi
// AI (gateway OpenAI-compatible + antrean pekerja Muse), mesin
// pekerjaan diskusi, dan 111 tool bawaan untuk kursi (coding di
// sandbox, browser, GitHub, LSP/debug, MCP, memori, workflow).
//
// State runtime disimpan sebagai satu dokumen (data.json, atau SQLite
// kv-blob bila MUSEHOOD_STORAGE=sqlite) dan dilindungi satu mutex
// aplikasi; semua pekerjaan latar berjalan pada konteks aplikasi dan
// berhenti lewat Close.
package musehood
