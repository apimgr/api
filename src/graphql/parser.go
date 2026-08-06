package graphql

import (
	"fmt"
	"strconv"
	"strings"
)

// selection is one field selection inside a GraphQL operation: its name, an
// optional alias, its literal/variable arguments, and any nested
// selections (used to filter which sub-fields of an object result are
// returned to the client).
type selection struct {
	Alias      string
	Name       string
	Arguments  map[string]argValue
	Selections []selection
}

// argValue is a parsed GraphQL argument value. It is either a literal Go
// value (string/float64/bool/nil) or a reference to a request variable
// (`$name`), resolved against the request's `variables` map at execution
// time.
type argValue struct {
	isVariable bool
	varName    string
	literal    interface{}
}

// resolve returns the concrete Go value for an argument, substituting the
// matching request variable when the argument was written as `$name`.
func (a argValue) resolve(variables map[string]interface{}) interface{} {
	if a.isVariable {
		return variables[a.varName]
	}
	return a.literal
}

// operation is a parsed GraphQL document: its type (query/mutation) and its
// top-level field selections.
type operation struct {
	Type       string
	Selections []selection
}

// docParser is a minimal hand-rolled parser for the subset of the GraphQL
// language this project's schema needs: a single anonymous or named query/
// mutation operation, aliased field selections, nested selection sets, and
// scalar/variable arguments. It intentionally does not support fragments,
// directives, or multiple operations per document — the schema in
// graphql.go doesn't use them, and pulling in a full GraphQL library only
// to skip most of it isn't worth the dependency.
type docParser struct {
	input []rune
	pos   int
}

func newDocParser(input string) *docParser {
	return &docParser{input: []rune(input)}
}

// parseDocument parses a full GraphQL request body into a single operation.
func (p *docParser) parseDocument() (operation, error) {
	p.skipIgnored()
	if p.pos >= len(p.input) {
		return operation{}, fmt.Errorf("syntax error: empty query")
	}

	opType := "query"
	if p.consumeKeyword("mutation") {
		opType = "mutation"
	} else {
		p.consumeKeyword("query")
	}
	p.skipIgnored()

	// Optional operation name (anonymous shorthand omits it).
	if p.pos < len(p.input) && p.input[p.pos] != '{' && p.input[p.pos] != '(' {
		p.parseName()
		p.skipIgnored()
	}

	// Optional variable definitions, e.g. "mutation($t: String!) { ... }".
	if err := p.parseVariableDefinitions(); err != nil {
		return operation{}, err
	}
	p.skipIgnored()

	sels, err := p.parseSelectionSet()
	if err != nil {
		return operation{}, err
	}
	return operation{Type: opType, Selections: sels}, nil
}

// parseVariableDefinitions consumes an optional operation variable
// declaration list, e.g. "($id: ID!, $limit: Int = 10)". Declared types and
// defaults are not tracked — arguments resolve variables directly from the
// request's "variables" map — so this only needs to skip the list,
// respecting nested brackets and string literals along the way.
func (p *docParser) parseVariableDefinitions() error {
	if p.pos >= len(p.input) || p.input[p.pos] != '(' {
		return nil
	}
	depth := 0
	for p.pos < len(p.input) {
		c := p.input[p.pos]
		if c == '"' {
			if _, err := p.parseString(); err != nil {
				return err
			}
			continue
		}
		if c == '(' {
			depth++
		} else if c == ')' {
			depth--
		}
		p.pos++
		if depth == 0 {
			return nil
		}
	}
	return fmt.Errorf("syntax error: unterminated variable definition list")
}

func (p *docParser) parseSelectionSet() ([]selection, error) {
	if err := p.expect('{'); err != nil {
		return nil, err
	}
	var sels []selection
	for {
		p.skipIgnored()
		if p.pos >= len(p.input) {
			return nil, fmt.Errorf("syntax error: unterminated selection set")
		}
		if p.input[p.pos] == '}' {
			p.pos++
			return sels, nil
		}
		sel, err := p.parseSelection()
		if err != nil {
			return nil, err
		}
		sels = append(sels, sel)
	}
}

func (p *docParser) parseSelection() (selection, error) {
	first := p.parseName()
	if first == "" {
		return selection{}, fmt.Errorf("syntax error: expected field name at position %d", p.pos)
	}
	p.skipIgnored()

	name := first
	alias := first
	if p.pos < len(p.input) && p.input[p.pos] == ':' {
		p.pos++
		p.skipIgnored()
		name = p.parseName()
		if name == "" {
			return selection{}, fmt.Errorf("syntax error: expected field name after alias %q", alias)
		}
		p.skipIgnored()
	}

	sel := selection{Alias: alias, Name: name}

	if p.pos < len(p.input) && p.input[p.pos] == '(' {
		args, err := p.parseArguments()
		if err != nil {
			return selection{}, err
		}
		sel.Arguments = args
		p.skipIgnored()
	}

	if p.pos < len(p.input) && p.input[p.pos] == '{' {
		sub, err := p.parseSelectionSet()
		if err != nil {
			return selection{}, err
		}
		sel.Selections = sub
	}

	return sel, nil
}

func (p *docParser) parseArguments() (map[string]argValue, error) {
	if err := p.expect('('); err != nil {
		return nil, err
	}
	args := map[string]argValue{}
	for {
		p.skipIgnored()
		if p.pos >= len(p.input) {
			return nil, fmt.Errorf("syntax error: unterminated argument list")
		}
		if p.input[p.pos] == ')' {
			p.pos++
			return args, nil
		}
		name := p.parseName()
		if name == "" {
			return nil, fmt.Errorf("syntax error: expected argument name at position %d", p.pos)
		}
		p.skipIgnored()
		if err := p.expect(':'); err != nil {
			return nil, err
		}
		p.skipIgnored()
		val, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		args[name] = val
		p.skipIgnored()
	}
}

func (p *docParser) parseValue() (argValue, error) {
	if p.pos >= len(p.input) {
		return argValue{}, fmt.Errorf("syntax error: expected value at position %d", p.pos)
	}

	switch c := p.input[p.pos]; {
	case c == '$':
		p.pos++
		name := p.parseName()
		if name == "" {
			return argValue{}, fmt.Errorf("syntax error: expected variable name at position %d", p.pos)
		}
		return argValue{isVariable: true, varName: name}, nil
	case c == '"':
		s, err := p.parseString()
		if err != nil {
			return argValue{}, err
		}
		return argValue{literal: s}, nil
	case c == '-' || (c >= '0' && c <= '9'):
		return p.parseNumber()
	case strings.HasPrefix(string(p.input[p.pos:]), "true"):
		p.pos += 4
		return argValue{literal: true}, nil
	case strings.HasPrefix(string(p.input[p.pos:]), "false"):
		p.pos += 5
		return argValue{literal: false}, nil
	case strings.HasPrefix(string(p.input[p.pos:]), "null"):
		p.pos += 4
		return argValue{literal: nil}, nil
	default:
		return argValue{}, fmt.Errorf("syntax error: unexpected character %q at position %d", c, p.pos)
	}
}

func (p *docParser) parseString() (string, error) {
	if err := p.expect('"'); err != nil {
		return "", err
	}
	var b strings.Builder
	for {
		if p.pos >= len(p.input) {
			return "", fmt.Errorf("syntax error: unterminated string literal")
		}
		c := p.input[p.pos]
		if c == '"' {
			p.pos++
			return b.String(), nil
		}
		if c == '\\' && p.pos+1 < len(p.input) {
			p.pos++
			switch p.input[p.pos] {
			case '"':
				b.WriteRune('"')
			case '\\':
				b.WriteRune('\\')
			case 'n':
				b.WriteRune('\n')
			case 't':
				b.WriteRune('\t')
			default:
				b.WriteRune(p.input[p.pos])
			}
			p.pos++
			continue
		}
		b.WriteRune(c)
		p.pos++
	}
}

func (p *docParser) parseNumber() (argValue, error) {
	start := p.pos
	if p.input[p.pos] == '-' {
		p.pos++
	}
	isFloat := false
	for p.pos < len(p.input) {
		c := p.input[p.pos]
		if c >= '0' && c <= '9' {
			p.pos++
			continue
		}
		if c == '.' && !isFloat {
			isFloat = true
			p.pos++
			continue
		}
		break
	}
	raw := string(p.input[start:p.pos])
	if isFloat {
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return argValue{}, fmt.Errorf("syntax error: invalid number %q", raw)
		}
		return argValue{literal: f}, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return argValue{}, fmt.Errorf("syntax error: invalid number %q", raw)
	}
	return argValue{literal: n}, nil
}

// parseName consumes a GraphQL name token: [_A-Za-z][_0-9A-Za-z]*
func (p *docParser) parseName() string {
	start := p.pos
	if p.pos >= len(p.input) {
		return ""
	}
	c := p.input[p.pos]
	if !isNameStart(c) {
		return ""
	}
	p.pos++
	for p.pos < len(p.input) && isNameChar(p.input[p.pos]) {
		p.pos++
	}
	return string(p.input[start:p.pos])
}

func isNameStart(c rune) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isNameChar(c rune) bool {
	return isNameStart(c) || (c >= '0' && c <= '9')
}

// consumeKeyword advances past kw only if it appears at the current
// position as a standalone name token (not a prefix of a longer name).
func (p *docParser) consumeKeyword(kw string) bool {
	save := p.pos
	name := p.parseName()
	if name == kw {
		return true
	}
	p.pos = save
	return false
}

func (p *docParser) expect(c rune) error {
	if p.pos >= len(p.input) || p.input[p.pos] != c {
		return fmt.Errorf("syntax error: expected %q at position %d", c, p.pos)
	}
	p.pos++
	return nil
}

// skipIgnored advances past whitespace, commas, and `#`-prefixed comments —
// all of which the GraphQL spec treats as insignificant between tokens.
func (p *docParser) skipIgnored() {
	for p.pos < len(p.input) {
		c := p.input[p.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ',':
			p.pos++
		case c == '#':
			for p.pos < len(p.input) && p.input[p.pos] != '\n' {
				p.pos++
			}
		default:
			return
		}
	}
}
