package fun

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
)

// Service provides fun/entertainment utilities
type Service struct{}

// New creates a new Fun service
func New() *Service {
	return &Service{}
}

// Dice rolling
func (s *Service) RollDice(sides int) (int, error) {
	if sides < 2 {
		return 0, fmt.Errorf("dice must have at least 2 sides")
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(sides)))
	if err != nil {
		return 0, err
	}
	return int(n.Int64()) + 1, nil
}

func (s *Service) RollMultipleDice(count, sides int) ([]int, error) {
	if count < 1 {
		return nil, fmt.Errorf("must roll at least 1 die")
	}
	results := make([]int, count)
	for i := 0; i < count; i++ {
		roll, err := s.RollDice(sides)
		if err != nil {
			return nil, err
		}
		results[i] = roll
	}
	return results, nil
}

// Coin flip
func (s *Service) CoinFlip() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(2))
	if err != nil {
		return "", err
	}
	if n.Int64() == 0 {
		return "heads", nil
	}
	return "tails", nil
}

// Random choice
func (s *Service) RandomChoice(options []string) (string, error) {
	if len(options) == 0 {
		return "", fmt.Errorf("no options provided")
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(options))))
	if err != nil {
		return "", err
	}
	return options[n.Int64()], nil
}

// 8-ball responses
var eightBallResponses = []string{
	"It is certain",
	"It is decidedly so",
	"Without a doubt",
	"Yes definitely",
	"You may rely on it",
	"As I see it, yes",
	"Most likely",
	"Outlook good",
	"Yes",
	"Signs point to yes",
	"Reply hazy, try again",
	"Ask again later",
	"Better not tell you now",
	"Cannot predict now",
	"Concentrate and ask again",
	"Don't count on it",
	"My reply is no",
	"My sources say no",
	"Outlook not so good",
	"Very doubtful",
}

func (s *Service) Magic8Ball() (string, error) {
	return s.RandomChoice(eightBallResponses)
}

// Fortune cookie
var fortunes = []string{
	"A beautiful, smart, and loving person will be coming into your life.",
	"A dubious friend may be an enemy in camouflage.",
	"A fresh start will put you on your way.",
	"A friend asks only for your time not your money.",
	"A gambler not only will lose what he has, but also will lose what he doesn't have.",
	"A golden egg of opportunity falls into your lap this month.",
	"A good time to finish up old tasks.",
	"A hunch is creativity trying to tell you something.",
	"A lifetime of happiness lies ahead of you.",
	"A light heart carries you through all the hard times.",
	"A new perspective will come with the new year.",
	"A person is never too old to learn.",
	"A smile is your passport into the hearts of others.",
	"Adventure can be real happiness.",
	"All your hard work will soon pay off.",
	"An exciting opportunity lies ahead.",
	"Be patient and you will be rewarded.",
	"Believe in yourself and others will too.",
	"Better days are coming.",
	"Change is happening in your life, so go with the flow!",
}

func (s *Service) Fortune() (string, error) {
	return s.RandomChoice(fortunes)
}

// Yes/No
func (s *Service) YesOrNo() (string, error) {
	return s.RandomChoice([]string{"yes", "no"})
}

// Random emoji
var emojis = []string{
	"😀", "😃", "😄", "😁", "😆", "😅", "🤣", "😂", "🙂", "🙃",
	"😉", "😊", "😇", "🥰", "😍", "🤩", "😘", "😗", "😚", "😙",
	"😋", "😛", "😜", "🤪", "😝", "🤑", "🤗", "🤭", "🤫", "🤔",
	"🤐", "🤨", "😐", "😑", "😶", "😏", "😒", "🙄", "😬", "🤥",
	"😌", "😔", "😪", "🤤", "😴", "😷", "🤒", "🤕", "🤢", "🤮",
}

func (s *Service) RandomEmoji() (string, error) {
	return s.RandomChoice(emojis)
}

// Random joke type
var jokeTypes = []string{
	"dad joke",
	"pun",
	"knock-knock",
	"programming joke",
	"one-liner",
}

func (s *Service) RandomJokeType() (string, error) {
	return s.RandomChoice(jokeTypes)
}

// Shuffle array
func (s *Service) Shuffle(items []string) ([]string, error) {
	result := make([]string, len(items))
	copy(result, items)
	
	for i := len(result) - 1; i > 0; i-- {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return nil, err
		}
		j := int(n.Int64())
		result[i], result[j] = result[j], result[i]
	}
	
	return result, nil
}

// Rock Paper Scissors
func (s *Service) RockPaperScissors(choice string) (string, error) {
	choice = strings.ToLower(strings.TrimSpace(choice))
	validChoices := []string{"rock", "paper", "scissors"}
	
	// Validate user choice
	valid := false
	for _, v := range validChoices {
		if choice == v {
			valid = true
			break
		}
	}
	if !valid {
		return "", fmt.Errorf("invalid choice: must be rock, paper, or scissors")
	}
	
	// Computer choice
	computerChoice, err := s.RandomChoice(validChoices)
	if err != nil {
		return "", err
	}
	
	// Determine winner
	if choice == computerChoice {
		return fmt.Sprintf("Draw! Both chose %s", choice), nil
	}
	
	if (choice == "rock" && computerChoice == "scissors") ||
		(choice == "paper" && computerChoice == "rock") ||
		(choice == "scissors" && computerChoice == "paper") {
		return fmt.Sprintf("You win! %s beats %s", choice, computerChoice), nil
	}
	
	return fmt.Sprintf("Computer wins! %s beats %s", computerChoice, choice), nil
}
