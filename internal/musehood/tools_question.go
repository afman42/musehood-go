package musehood

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
)

// --- Tool ask_question (gaya OpenCode "question") ---
//
// Kursi berhenti dan bertanya kepada pemilik dengan pilihan jawaban.
// Pekerjaan kursi MENUNGGU jawaban (timeout sama seperti persetujuan,
// approvalTimeout); timeout menghasilkan teks "tidak dijawab" agar kursi
// dapat melanjutkan dengan asumsi terbaiknya. Pemilik menjawab lewat
// banner pertanyaan di frontend atau POST /api/questions/{id}
// {"answer": "..."}. Jawaban bebas selalu tersedia selain pilihan.

const (
	maxQuestionChars  = 1000
	maxQuestionOption = 6
	maxOptionChars    = 200
	maxAnswerChars    = 2000
)

// question adalah pertanyaan kursi yang menunggu jawaban pemilik.
type question struct {
	ID        string   `json:"id"`
	SeatCode  string   `json:"seatCode"`
	SeatName  string   `json:"seatName"`
	Text      string   `json:"text"`
	Options   []string `json:"options,omitempty"`
	CreatedAt string   `json:"createdAt"`
	done      chan string
}

// requestAnswer mendaftarkan pertanyaan dan menunggu jawaban pemilik.
func (a *App) requestAnswer(ctx context.Context, s seat, text string, options []string) (string, bool) {
	q := &question{
		ID: uuid(), SeatCode: s.Code, SeatName: s.Name,
		Text: text, Options: options, CreatedAt: now(),
		done: make(chan string, 1),
	}
	a.questionMu.Lock()
	if a.questions == nil {
		a.questions = map[string]*question{}
	}
	a.questions[q.ID] = q
	a.questionMu.Unlock()
	defer func() {
		a.questionMu.Lock()
		delete(a.questions, q.ID)
		a.questionMu.Unlock()
	}()
	select {
	case answer := <-q.done:
		return answer, true
	case <-time.After(approvalTimeout):
		return "", false
	case <-ctx.Done():
		return "", false
	}
}

func (a *App) pendingQuestions() []question {
	a.questionMu.Lock()
	defer a.questionMu.Unlock()
	out := make([]question, 0, len(a.questions))
	for _, q := range a.questions {
		out = append(out, *q)
	}
	slices.SortFunc(out, func(a, b question) int { return cmp.Compare(a.CreatedAt, b.CreatedAt) })
	return out
}

// answerQuestion menyampaikan jawaban pemilik ke pertanyaan tertunda.
func (a *App) answerQuestion(id, answer string) bool {
	a.questionMu.Lock()
	defer a.questionMu.Unlock()
	q, ok := a.questions[id]
	if !ok {
		return false
	}
	select {
	case q.done <- answer:
	default:
	}
	return true
}

// parseQuestionArg mengurai argumen ask_question: baris pertama adalah
// pertanyaan, baris-baris berikutnya yang tidak kosong adalah pilihan
// jawaban (maks 6).
func parseQuestionArg(arg string) (string, []string, error) {
	lines := strings.Split(arg, "\n")
	text := strings.TrimSpace(lines[0])
	if text == "" {
		return "", nil, fmt.Errorf("pertanyaan kosong")
	}
	if len([]rune(text)) > maxQuestionChars {
		return "", nil, fmt.Errorf("pertanyaan terlalu panjang (maks %d karakter)", maxQuestionChars)
	}
	var options []string
	for _, line := range lines[1:] {
		option := strings.TrimSpace(line)
		if option == "" {
			continue
		}
		if len([]rune(option)) > maxOptionChars {
			return "", nil, fmt.Errorf("pilihan terlalu panjang (maks %d karakter)", maxOptionChars)
		}
		options = append(options, option)
		if len(options) > maxQuestionOption {
			return "", nil, fmt.Errorf("maksimal %d pilihan jawaban", maxQuestionOption)
		}
	}
	return text, options, nil
}

func (a *App) toolAskQuestion(ctx context.Context, s seat, arg string) string {
	text, options, err := parseQuestionArg(arg)
	if err != nil {
		return "Error: " + err.Error()
	}
	answer, ok := a.requestAnswer(ctx, s, text, options)
	if !ok {
		return fmt.Sprintf("Pemilik tidak menjawab dalam %d detik. Lanjutkan dengan asumsi terbaikmu dan sebutkan asumsi itu dalam jawaban.", int(approvalTimeout.Seconds()))
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return "Pemilik menjawab kosong. Lanjutkan dengan asumsi terbaikmu."
	}
	return "Jawaban pemilik: " + truncateRunes(answer, maxAnswerChars)
}
