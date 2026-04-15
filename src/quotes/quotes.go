package quotes

import (
	"fmt"
	"math/rand/v2"
	"os"
	"sync"
)

type Category string

const (
	Init    Category = "init"
	Up      Category = "up"
	Down    Category = "down"
	Error   Category = "error"
	Success Category = "success"
	General Category = "general"
)

var initQuotes = []string{
	"Wake up, Neo.",
	"The Matrix has you.",
	"Follow the white rabbit.",
	"Blue pill or red pill?",
	"You have to see it for yourself.",
	"This is your last chance. After this, there is no turning back.",
	"I've had dreams that weren't just dreams.",
	"Knock, knock, Neo.",
}

var upQuotes = []string{
	"I know kung fu.",
	"I still know kung fu.",
	"There is no spoon.",
	"Dodge this.",
	"He's beginning to believe.",
	"Free your mind.",
	"Tank, load the jump program.",
	"Everything begins with choice.",
	"Because I choose to.",
	"Were you listening to me, Neo? Or were you looking at the woman in the red dress?",
	"Fasten your seatbelt, Dorothy, 'cause Kansas is going bye-bye.",
}

var downQuotes = []string{
	"Everything that has a beginning has an end.",
	"It ends tonight.",
	"The purpose of life is to end.",
	"Mr. Wizard, get me out of here!",
	"We're going to need an exit.",
	"There is no turning back.",
	"Goodbye, Mr. Anderson.",
}

var errorQuotes = []string{
	"Not like this... not like this.",
	"You can't win.",
	"It's pointless to keep fighting.",
	"Choice is an illusion...",
	"A deja vu is usually a glitch in the Matrix.",
	"The system is our enemy.",
	"Aye-yi-yi, what a mess.",
}

var successQuotes = []string{
	"Welcome to the real world.",
	"A world where anything is possible.",
	"The ones that want out... will be freed.",
	"He is the One.",
	"You've been down there, Neo. You know that road.",
}

var quotes = map[Category][]string{
	Init:    initQuotes,
	Up:      upQuotes,
	Down:    downQuotes,
	Error:   errorQuotes,
	Success: successQuotes,
	General: generalQuotes,
}

var generalQuotes = []string{
	"Mr. Anderson.",
	"The Matrix has you.",
	"There is no spoon.",
	"Choice. The problem is choice.",
	"Guns. Lots of guns.",
	"Everything that has a beginning has an end.",
	"I still know kung fu.",
	"Because I choose to.",
}

var (
	queueMu sync.Mutex
	queues  = make(map[Category][]string)
)

// Get returns the next quote for the given category.
// Cycles through all quotes in shuffled order before repeating.
func Get(cat Category) string {
	q, ok := quotes[cat]
	if !ok || len(q) == 0 {
		return ""
	}

	queueMu.Lock()
	defer queueMu.Unlock()

	// Refill and shuffle when queue is empty
	if len(queues[cat]) == 0 {
		shuffled := make([]string, len(q))
		copy(shuffled, q)
		rand.Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})
		queues[cat] = shuffled
	}

	// Pop from queue
	queue := queues[cat]
	quote := queue[0]
	queues[cat] = queue[1:]
	return quote
}

// Print prints a random quote for the given category to stderr in Matrix green.
func Print(cat Category) {
	if os.Getenv("JACKIN_QUOTES") == "0" {
		return
	}
	if q := Get(cat); q != "" {
		fmt.Fprintf(os.Stderr, "\n\033[38;5;34m%s\033[0m\n\n", q)
	}
}
