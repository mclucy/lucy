package ansi

import (
	"embed"
	"io"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/alecthomas/chroma/v2"
)

// The build imports only these lexers; importing chroma/lexers would register
// every language and embed its complete XML registry.
//
//go:embed lexers/*.xml
var lexerFiles embed.FS

var (
	subsetLexers = map[string]chroma.Lexer{}
	subsetFiles  []fileLexer
)

type fileLexer struct {
	lexer     chroma.Lexer
	filenames []string
}

func init() {
	paths, err := fs.Glob(lexerFiles, "lexers/*.xml")
	if err != nil {
		panic(err)
	}
	for _, path := range paths {
		lexer, err := chroma.NewXMLLexer(lexerFiles, path)
		if err != nil {
			panic(err)
		}
		config := lexer.Config()
		subsetLexers[strings.ToLower(config.Name)] = lexer
		for _, alias := range config.Aliases {
			subsetLexers[strings.ToLower(alias)] = lexer
		}
		subsetFiles = append(subsetFiles, fileLexer{
			lexer:     lexer,
			filenames: append(append([]string{}, config.Filenames...), config.AliasFilenames...),
		})
	}
}

// lexerFor resolves a fence language the way chroma's registry does: name,
// alias, then a filename glob such as *.yml. Unknown languages use plaintext.
func lexerFor(language string) chroma.Lexer {
	language = strings.TrimSpace(language)
	if lexer := subsetLexers[strings.ToLower(language)]; lexer != nil {
		return lexer
	}
	for _, candidate := range []string{"filename." + language, language} {
		for _, entry := range subsetFiles {
			for _, glob := range entry.filenames {
				if ok, _ := filepath.Match(glob, candidate); ok {
					return entry.lexer
				}
			}
		}
	}
	return subsetLexers["plaintext"]
}

// renderHighlighted writes code with the token colors declared by the Markdown
// style. A missing or unknown language uses the plaintext lexer.
func renderHighlighted(w io.Writer, code, language string, style Chroma) error {
	lexer := lexerFor(language)
	if lexer == nil {
		_, err := io.WriteString(w, code)
		return err
	}
	iterator, err := chroma.Coalesce(lexer).Tokenise(nil, code)
	if err != nil {
		return err
	}
	for token := iterator(); token != chroma.EOF; token = iterator() {
		if _, err := renderText(w, tokenStyle(style, token.Type), token.Value); err != nil {
			return err
		}
	}
	return nil
}

// tokenStyle walks the token hierarchy until it finds a configured style, the
// same inheritance chroma's formatter applies.
func tokenStyle(style Chroma, token chroma.TokenType) StylePrimitive {
	for {
		var primitive StylePrimitive
		matched := true
		switch token {
		case chroma.Background:
			primitive = style.Text
		case chroma.Error:
			primitive = style.Error
		case chroma.Comment:
			primitive = style.Comment
		case chroma.CommentPreproc:
			primitive = style.CommentPreproc
		case chroma.Keyword:
			primitive = style.Keyword
		case chroma.KeywordReserved:
			primitive = style.KeywordReserved
		case chroma.KeywordNamespace:
			primitive = style.KeywordNamespace
		case chroma.KeywordType:
			primitive = style.KeywordType
		case chroma.Operator:
			primitive = style.Operator
		case chroma.Punctuation:
			primitive = style.Punctuation
		case chroma.Name:
			primitive = style.Name
		case chroma.NameBuiltin:
			primitive = style.NameBuiltin
		case chroma.NameTag:
			primitive = style.NameTag
		case chroma.NameAttribute:
			primitive = style.NameAttribute
		case chroma.NameClass:
			primitive = style.NameClass
		case chroma.NameConstant:
			primitive = style.NameConstant
		case chroma.NameDecorator:
			primitive = style.NameDecorator
		case chroma.NameException:
			primitive = style.NameException
		case chroma.NameFunction:
			primitive = style.NameFunction
		case chroma.NameOther:
			primitive = style.NameOther
		case chroma.Literal:
			primitive = style.Literal
		case chroma.LiteralNumber:
			primitive = style.LiteralNumber
		case chroma.LiteralDate:
			primitive = style.LiteralDate
		case chroma.LiteralString:
			primitive = style.LiteralString
		case chroma.LiteralStringEscape:
			primitive = style.LiteralStringEscape
		case chroma.GenericDeleted:
			primitive = style.GenericDeleted
		case chroma.GenericEmph:
			primitive = style.GenericEmph
		case chroma.GenericInserted:
			primitive = style.GenericInserted
		case chroma.GenericStrong:
			primitive = style.GenericStrong
		case chroma.GenericSubheading:
			primitive = style.GenericSubheading
		case chroma.Text:
			primitive = style.Text
		default:
			matched = false
		}
		if matched && styleConfigured(primitive) {
			return primitive
		}
		if token <= 0 {
			return StylePrimitive{}
		}
		token = token.Parent()
	}
}

// styleConfigured reports whether the style sets anything. Empty entries inherit
// from their parent token type instead of rendering unstyled.
func styleConfigured(style StylePrimitive) bool {
	return style.Color != nil ||
		style.BackgroundColor != nil ||
		style.Underline != nil ||
		style.Bold != nil ||
		style.Upper != nil ||
		style.Lower != nil ||
		style.Title != nil ||
		style.Italic != nil ||
		style.CrossedOut != nil ||
		style.Faint != nil ||
		style.Conceal != nil ||
		style.Inverse != nil ||
		style.Blink != nil ||
		style.Format != ""
}
