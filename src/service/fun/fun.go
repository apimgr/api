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

// randomIndex picks a cryptographically random index in [0, n).
func (s *Service) randomIndex(n int) (int, error) {
	if n <= 0 {
		return 0, fmt.Errorf("no options provided")
	}
	idx, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0, err
	}
	return int(idx.Int64()), nil
}

// Dad jokes
var dadJokes = []string{
	"I'm afraid for the calendar. Its days are numbered.",
	"Why don't skeletons fight each other? They don't have the guts.",
	"I used to hate facial hair, but then it grew on me.",
	"What do you call a fish wearing a bowtie? Sofishticated.",
	"I only know 25 letters of the alphabet. I don't know y.",
	"Why did the scarecrow win an award? He was outstanding in his field.",
	"I'm reading a book about anti-gravity. It's impossible to put down.",
	"What do you call a factory that makes okay products? A satisfactory.",
	"I told my wife she was drawing her eyebrows too high. She looked surprised.",
	"Why don't eggs tell jokes? They'd crack each other up.",
	"What do you call a bear with no teeth? A gummy bear.",
	"I used to be a banker, but I lost interest.",
	"Why did the bicycle fall over? It was two-tired.",
	"What do you call cheese that isn't yours? Nacho cheese.",
	"I would tell you a chemistry joke, but I know I wouldn't get a reaction.",
	"Why can't your nose be 12 inches long? Because then it'd be a foot.",
	"What did the ocean say to the beach? Nothing, it just waved.",
	"I'm on a seafood diet. I see food and I eat it.",
	"Why did the golfer bring two pairs of pants? In case he got a hole in one.",
	"What do you call a dinosaur that crashes his car? Tyrannosaurus wrecks.",
	"How do you organize a space party? You planet.",
	"I've started telling everyone about the benefits of eating dried grapes. It's all about raisin awareness.",
	"What do you call a can opener that doesn't work? A can't opener.",
	"Why did the coffee file a police report? It got mugged.",
	"What do you call a pig that does karate? A pork chop.",
	"I'm terrified of elevators, so I'm going to start taking steps to avoid them.",
	"What do you call an alligator in a vest? An investigator.",
	"Why did the man put his money in the freezer? He wanted cold hard cash.",
	"What's brown and sticky? A stick.",
	"How does a penguin build its house? Igloos it together.",
}

func (s *Service) DadJoke() (string, error) {
	return s.RandomChoice(dadJokes)
}

// Programming jokes
var programmingJokes = []string{
	"Why do programmers prefer dark mode? Because light attracts bugs.",
	"There are 10 types of people: those who understand binary and those who don't.",
	"Why do Java developers wear glasses? Because they don't C#.",
	"A SQL query walks into a bar, walks up to two tables and asks: 'Can I join you?'",
	"In order to understand recursion, you must first understand recursion.",
	"Why do programmers always mix up Halloween and Christmas? Because Oct 31 == Dec 25.",
	"How many programmers does it take to change a light bulb? None, that's a hardware problem.",
	"A programmer's wife tells him: 'Go to the store and buy a loaf of bread. If they have eggs, buy a dozen.' He comes home with 12 loaves of bread.",
	"Why did the developer go broke? Because he used up all his cache.",
	"What's the object-oriented way to become wealthy? Inheritance.",
	"!false — it's funny because it's true.",
	"Why was the JavaScript developer sad? Because he didn't Node how to Express himself.",
	"There's no place like 127.0.0.1.",
	"Why do programmers hate nature? It has too many bugs and no debugger.",
	"I would tell a UDP joke, but you might not get it.",
	"A byte walks into a bar looking miserable. The bartender asks what's wrong. The byte says, 'Parity error.' The bartender says, 'I thought you looked a bit off.'",
	"Why did the programmer quit his job? He didn't get arrays.",
	"What do you call 8 hobbits? A hobbyte.",
	"Real programmers count from 0.",
	"Why do Python programmers prefer snake_case? Because camelCase gives them a hump.",
	"I told my computer I needed a break, and now it won't stop sending me KitKat ads.",
	"How do you comfort a JavaScript bug? You console it.",
	"Why did the developer stare at the empty coffee cup? Because it was null and void.",
	"Debugging is like being the detective in a crime movie where you are also the murderer.",
	"It works on my machine.",
}

func (s *Service) ProgrammingJoke() (string, error) {
	return s.RandomChoice(programmingJokes)
}

// Quotes
var quotes = []string{
	"The only way to do great work is to love what you do. - Steve Jobs",
	"Life is what happens when you're busy making other plans. - John Lennon",
	"The future belongs to those who believe in the beauty of their dreams. - Eleanor Roosevelt",
	"It is during our darkest moments that we must focus to see the light. - Aristotle",
	"Whoever is happy will make others happy too. - Anne Frank",
	"Do not go where the path may lead, go instead where there is no path and leave a trail. - Ralph Waldo Emerson",
	"In the middle of difficulty lies opportunity. - Albert Einstein",
	"The way to get started is to quit talking and begin doing. - Walt Disney",
	"Life is really simple, but we insist on making it complicated. - Confucius",
	"You only live once, but if you do it right, once is enough. - Mae West",
	"Many of life's failures are people who did not realize how close they were to success when they gave up. - Thomas Edison",
	"If you want to lift yourself up, lift up someone else. - Booker T. Washington",
	"The best time to plant a tree was 20 years ago. The second best time is now. - Chinese Proverb",
	"Do what you can, with what you have, where you are. - Theodore Roosevelt",
	"Nothing is impossible, the word itself says 'I'm possible'. - Audrey Hepburn",
	"Success is not final, failure is not fatal: it is the courage to continue that counts. - Winston Churchill",
	"You miss 100% of the shots you don't take. - Wayne Gretzky",
	"Whether you think you can or you think you can't, you're right. - Henry Ford",
	"The only impossible journey is the one you never begin. - Tony Robbins",
	"In three words I can sum up everything I've learned about life: it goes on. - Robert Frost",
	"Spread love everywhere you go. - Mother Teresa",
	"When you reach the end of your rope, tie a knot in it and hang on. - Franklin D. Roosevelt",
	"Always remember that you are absolutely unique. Just like everyone else. - Margaret Mead",
	"Don't judge each day by the harvest you reap but by the seeds that you plant. - Robert Louis Stevenson",
	"The best and most beautiful things in the world cannot be seen or even touched - they must be felt with the heart. - Helen Keller",
}

func (s *Service) Quote() (string, error) {
	return s.RandomChoice(quotes)
}

// Facts
var facts = []string{
	"Honey never spoils; archaeologists have found 3,000-year-old honey in Egyptian tombs that's still edible.",
	"A group of flamingos is called a 'flamboyance'.",
	"Octopuses have three hearts and blue blood.",
	"Bananas are berries, but strawberries aren't.",
	"The Eiffel Tower can grow more than 6 inches taller in summer due to thermal expansion.",
	"A single bolt of lightning contains enough energy to toast about 100,000 slices of bread.",
	"Sea otters hold hands while sleeping so they don't drift apart.",
	"The shortest war in recorded history lasted just 38 minutes, between Britain and Zanzibar in 1896.",
	"Wombat poop is cube-shaped.",
	"There are more possible iterations of a game of chess than atoms in the observable universe.",
	"The human nose can detect over 1 trillion distinct scents.",
	"A day on Venus is longer than a year on Venus.",
	"Sharks existed before trees.",
	"The inventor of the frisbee was cremated and turned into a frisbee after he died.",
	"It rains diamonds on Jupiter and Saturn.",
	"Cows have best friends and get stressed when separated from them.",
	"The unicorn is the national animal of Scotland.",
	"Some cats are allergic to humans.",
	"A bolt of lightning is roughly five times hotter than the surface of the sun.",
	"Butterflies taste with their feet.",
	"The world's oldest known living tree is over 5,000 years old.",
	"Antarctica is the largest desert in the world.",
	"Humans share about 60% of their DNA with bananas.",
	"The Great Wall of China is not visible from space with the naked eye, contrary to popular belief.",
	"An octopus can change both the color and texture of its skin in less than a second.",
}

func (s *Service) Fact() (string, error) {
	return s.RandomChoice(facts)
}

// Motivational quotes
var motivationalQuotes = []string{
	"Push yourself, because no one else is going to do it for you.",
	"Great things never come from comfort zones.",
	"Dream it. Wish it. Do it.",
	"Success doesn't just find you. You have to go out and get it.",
	"The harder you work for something, the greater you'll feel when you achieve it.",
	"Don't stop when you're tired. Stop when you're done.",
	"Wake up with determination. Go to bed with satisfaction.",
	"Do something today that your future self will thank you for.",
	"Little things make big days.",
	"It's going to be hard, but hard does not mean impossible.",
	"Don't wait for opportunity. Create it.",
	"Sometimes we're tested not to show our weaknesses, but to discover our strengths.",
	"The key to success is to focus on goals, not obstacles.",
	"Dream bigger. Do bigger.",
	"Your only limit is you.",
	"Every accomplishment starts with the decision to try.",
	"Believe you can and you're halfway there.",
	"A little progress each day adds up to big results.",
	"Difficult roads often lead to beautiful destinations.",
	"What you get by achieving your goals is not as important as what you become by achieving your goals.",
	"Start where you are. Use what you have. Do what you can.",
	"The only limit to our realization of tomorrow will be our doubts of today.",
	"Act as if what you do makes a difference. It does.",
	"You are never too old to set another goal or to dream a new dream.",
	"Opportunities don't happen. You create them.",
}

func (s *Service) Motivational() (string, error) {
	return s.RandomChoice(motivationalQuotes)
}

// Insults (playful, mock-Shakespearean/roast style — harmless, not offensive)
var insults = []string{
	"You're about as useful as a screen door on a submarine.",
	"You bring everyone so much joy... when you leave the room.",
	"I'd agree with you, but then we'd both be wrong.",
	"You're proof that even evolution takes a coffee break sometimes.",
	"You have the perfect face for radio.",
	"I've seen better ideas fall out of a cereal box.",
	"You're like a cloud — when you disappear, it's a beautiful day.",
	"If laughter is the best medicine, your face must be curing the world.",
	"You're not stupid; you just have bad luck thinking.",
	"You're about as sharp as a marble.",
	"I'm not saying you're slow, but snails send you postcards from the finish line.",
	"You're the human equivalent of a participation trophy.",
	"You have something on your chin... no, the third one down.",
	"You're like a software update — nobody wants you around, but you keep showing up.",
	"Your secrets are always safe with me. I never even listen when you tell me them.",
	"You're not the dumbest person on Earth, but you better hope they don't die.",
	"You have an entire life to be an idiot. Why not take today off?",
	"I would explain it to you, but I left my crayons at home.",
	"You're like a Monday morning — nobody's happy to see you.",
	"You bring so much to the table. Mostly crumbs.",
	"You're proof that autocorrect can't fix everything.",
	"You're the reason shampoo bottles have instructions.",
	"You have such a unique perspective. Mostly because nobody else thinks that way.",
	"You're about as pleasant as a paper cut.",
	"I'd explain the joke, but you're clearly the punchline.",
}

func (s *Service) Insult() (string, error) {
	return s.RandomChoice(insults)
}

// Compliments
var compliments = []string{
	"You have an amazing ability to make people feel comfortable.",
	"Your smile is contagious in the best way.",
	"You're a genuinely kind and thoughtful person.",
	"Your creativity knows no bounds.",
	"You have impeccable taste.",
	"Your positive energy is infectious.",
	"You always know how to make people laugh.",
	"You have a great sense of humor.",
	"You're an incredible listener.",
	"Your hard work never goes unnoticed.",
	"You light up every room you walk into.",
	"You're wiser than you realize.",
	"Your kindness is a gift to everyone around you.",
	"You inspire everyone around you to be better.",
	"You have a heart of gold.",
	"You're stronger than you know.",
	"Your ideas always bring something fresh to the table.",
	"You make the world a better place just by being in it.",
	"You have excellent judgment.",
	"You're incredibly talented at what you do.",
	"Your determination is admirable.",
	"You always find the silver lining.",
	"You have a brilliant mind.",
	"People are lucky to know you.",
	"Your confidence is inspiring.",
}

func (s *Service) Compliment() (string, error) {
	return s.RandomChoice(compliments)
}

// Meme captions (text-only classic meme caption formats; no image generation)
var memeCaptions = []string{
	"One does not simply walk into Mordor.",
	"I don't always test my code, but when I do, I do it in production.",
	"Y U NO comment your code?",
	"That moment when your code finally works and you have no idea why.",
	"Not sure if it's a feature or a bug.",
	"This is fine. (everything is on fire)",
	"Ain't nobody got time for that.",
	"But that's none of my business.",
	"Deal with it.",
	"Much wow. Very code. So bug.",
	"Brace yourselves, the deploy is coming.",
	"Is this a pigeon?",
	"Distracted boyfriend, but it's me looking at a new framework instead of finishing the project.",
	"Change my mind.",
	"Success kid: shipped it on the first try.",
	"Why not both?",
	"One simply does not merge to main on a Friday.",
	"Surprised Pikachu face when the bug was a missing semicolon.",
	"Doge: such feature, much wow, very deploy.",
	"Two buttons: fix the bug or ship the feature.",
	"Hide the pain Harold, reviewing legacy code.",
	"Drakeposting: rejecting the easy way, choosing the hard way.",
	"Galaxy brain: it works on my machine.",
	"Roll safe: can't have bugs if you never write tests.",
	"Expanding brain meme: print statement, then debugger, then rubber duck, then prayer.",
}

func (s *Service) Meme() (string, error) {
	return s.RandomChoice(memeCaptions)
}

// QAPair is a question/answer pair used by riddle and trivia generators.
type QAPair struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

// Riddles
var riddles = []QAPair{
	{Question: "What has keys but can't open locks?", Answer: "A piano."},
	{Question: "What has a face and two hands but no arms or legs?", Answer: "A clock."},
	{Question: "The more you take, the more you leave behind. What am I?", Answer: "Footsteps."},
	{Question: "What has to be broken before you can use it?", Answer: "An egg."},
	{Question: "I speak without a mouth and hear without ears. What am I?", Answer: "An echo."},
	{Question: "What gets wetter as it dries?", Answer: "A towel."},
	{Question: "What can travel around the world while staying in a corner?", Answer: "A stamp."},
	{Question: "What has one eye but cannot see?", Answer: "A needle."},
	{Question: "What comes down but never goes up?", Answer: "Rain."},
	{Question: "What has many teeth but cannot bite?", Answer: "A comb."},
	{Question: "What month of the year has 28 days?", Answer: "All of them."},
	{Question: "What has a neck but no head?", Answer: "A bottle."},
	{Question: "What can you catch but not throw?", Answer: "A cold."},
	{Question: "What has hands but cannot clap?", Answer: "A clock."},
	{Question: "What is full of holes but still holds water?", Answer: "A sponge."},
	{Question: "What building has the most stories?", Answer: "The library."},
	{Question: "What is so fragile that saying its name breaks it?", Answer: "Silence."},
	{Question: "I'm tall when I'm young and short when I'm old. What am I?", Answer: "A candle."},
	{Question: "What goes up but never comes down?", Answer: "Your age."},
	{Question: "What has a thumb and four fingers but is not alive?", Answer: "A glove."},
}

func (s *Service) Riddle() (QAPair, error) {
	i, err := s.randomIndex(len(riddles))
	if err != nil {
		return QAPair{}, err
	}
	return riddles[i], nil
}

// Trivia
var trivia = []QAPair{
	{Question: "What is the largest planet in our solar system?", Answer: "Jupiter."},
	{Question: "Which country has the most natural lakes?", Answer: "Canada."},
	{Question: "What is the smallest country in the world by area?", Answer: "Vatican City."},
	{Question: "Who painted the Mona Lisa?", Answer: "Leonardo da Vinci."},
	{Question: "What is the hardest natural substance on Earth?", Answer: "Diamond."},
	{Question: "How many bones are in the adult human body?", Answer: "206."},
	{Question: "What is the currency of Japan?", Answer: "The yen."},
	{Question: "Which planet is known as the Red Planet?", Answer: "Mars."},
	{Question: "What is the longest river in the world?", Answer: "The Nile."},
	{Question: "In what year did the Titanic sink?", Answer: "1912."},
	{Question: "What is the chemical symbol for gold?", Answer: "Au."},
	{Question: "Who wrote the play Romeo and Juliet?", Answer: "William Shakespeare."},
	{Question: "What is the fastest land animal?", Answer: "The cheetah."},
	{Question: "What ocean is the largest?", Answer: "The Pacific Ocean."},
	{Question: "How many continents are there?", Answer: "Seven."},
	{Question: "What is the capital of Australia?", Answer: "Canberra."},
	{Question: "Which gas do plants primarily absorb for photosynthesis?", Answer: "Carbon dioxide."},
	{Question: "What is the tallest mountain in the world?", Answer: "Mount Everest."},
	{Question: "Who developed the theory of general relativity?", Answer: "Albert Einstein."},
	{Question: "What is the smallest prime number?", Answer: "Two."},
}

func (s *Service) Trivia() (QAPair, error) {
	i, err := s.randomIndex(len(trivia))
	if err != nil {
		return QAPair{}, err
	}
	return trivia[i], nil
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
