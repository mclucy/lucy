//go:build ignore

// Command release-announce-discord posts a release announcement to the Discord
// webhook. The release body repeats installation instructions that the embed
// already carries, and Discord rejects descriptions over 4096 characters, so
// the announcement keeps the visible changelog and links out for the rest.
package main

import (
	"bufio"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"unicode/utf8"
)

const maxDescription = 3900

type release struct {
	Body         string `json:"body"`
	URL          string `json:"url"`
	IsPrerelease bool   `json:"isPrerelease"`
}

type allowedMentions struct {
	Parse []string `json:"parse"`
}

type embedField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type embedFooter struct {
	Text string `json:"text"`
}

type embed struct {
	Title       string       `json:"title"`
	URL         string       `json:"url"`
	Color       int          `json:"color"`
	Description string       `json:"description"`
	Fields      []embedField `json:"fields"`
	Footer      embedFooter  `json:"footer"`
}

type payload struct {
	AllowedMentions allowedMentions `json:"allowed_mentions"`
	Embeds          []embed         `json:"embeds"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "release-announce-discord: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	repo, err := required("GITHUB_REPOSITORY")
	if err != nil {
		return err
	}
	webhook, err := required("DISCORD_WEBHOOK_URL")
	if err != nil {
		return err
	}
	runnerTemp, err := required("RUNNER_TEMP")
	if err != nil {
		return err
	}

	tag := os.Getenv("INPUT_TAG")
	if tag == "" {
		tag = os.Getenv("GITHUB_REF_NAME")
	}

	raw, err := exec.Command("gh", "release", "view", tag, "--repo", repo,
		"--json", "body,url,isPrerelease").Output()
	if err != nil {
		return fmt.Errorf("read release %s: %w", tag, err)
	}
	var rel release
	if err := json.Unmarshal(raw, &rel); err != nil {
		return fmt.Errorf("parse release %s: %w", tag, err)
	}

	notes, err := changelog(rel.Body)
	if err != nil {
		return fmt.Errorf("filter release notes: %w", err)
	}

	title := "lucy " + tag
	color := 5793266
	if rel.IsPrerelease {
		title += " · pre-release"
		color = 16436250
	}
	message := payload{
		AllowedMentions: allowedMentions{Parse: []string{}},
		Embeds: []embed{{
			Title:       title,
			URL:         rel.URL,
			Color:       color,
			Description: description(notes, rel.URL),
			Fields: []embedField{{
				Name:  "Install",
				Value: "`curl -fsSL https://lucy.lc/sh | sh`",
			}},
			Footer: embedFooter{Text: "lucy.lc"},
		}},
	}
	data, err := json.Marshal(message, jsontext.Multiline(true), jsontext.WithIndent("  "))
	if err != nil {
		return fmt.Errorf("encode announcement: %w", err)
	}
	data = append(data, '\n')

	payloadFile, err := os.CreateTemp(runnerTemp, "discord-payload-*.json")
	if err != nil {
		return fmt.Errorf("create payload file: %w", err)
	}
	defer os.Remove(payloadFile.Name())
	if _, err := payloadFile.Write(data); err != nil {
		payloadFile.Close()
		return fmt.Errorf("write payload file: %w", err)
	}
	if err := payloadFile.Close(); err != nil {
		return fmt.Errorf("close payload file: %w", err)
	}

	responseFile, err := os.CreateTemp(runnerTemp, "discord-response-*.txt")
	if err != nil {
		return fmt.Errorf("create response file: %w", err)
	}
	defer os.Remove(responseFile.Name())
	if err := responseFile.Close(); err != nil {
		return fmt.Errorf("close response file: %w", err)
	}

	curl := exec.Command("curl", "-sS",
		"-o", responseFile.Name(),
		"-w", "%{http_code}",
		"-H", "Content-Type: application/json",
		"--retry", "3", "--retry-delay", "2",
		"--data-binary", "@"+payloadFile.Name(),
		webhook)
	curl.Stderr = os.Stderr
	status, err := curl.Output()
	if err != nil {
		return fmt.Errorf("post announcement: %w", err)
	}

	code := strings.TrimSpace(string(status))
	switch code {
	case "200", "204":
		fmt.Printf("Announced %s to Discord\n", tag)
	default:
		fmt.Fprintf(os.Stderr, "::error::Discord webhook returned HTTP %s\n", code)
		if response, err := os.ReadFile(responseFile.Name()); err == nil {
			os.Stderr.Write(response)
		}
		os.Exit(1)
	}
	return nil
}

func required(name string) (string, error) {
	value := os.Getenv(name)
	if value == "" {
		return "", fmt.Errorf("%s is not set", name)
	}
	return value, nil
}

// changelog applies the filters the announcement used to perform with sed:
// stop at the Installation section, drop collapsible blocks and empty headings.
func changelog(body string) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(body + "\n"))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var kept []string
	inDetails := false
	for scanner.Scan() {
		line := scanner.Text()
		if line == "## Installation" {
			break
		}
		if inDetails {
			if line == "</details>" {
				inDetails = false
			}
			continue
		}
		if strings.HasPrefix(line, "<details>") {
			inDetails = true
			continue
		}
		if line == "### " {
			continue
		}
		kept = append(kept, line)
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if len(kept) == 0 {
		return "", nil
	}
	return strings.Join(kept, "\n") + "\n", nil
}

func description(notes, url string) string {
	text := strings.TrimRight(notes, "\n")
	switch {
	case text == "":
		return "No user-visible changes."
	case utf8.RuneCountInString(text) > maxDescription:
		truncated := string([]rune(text)[:maxDescription])
		if lastNewline := strings.LastIndexByte(truncated, '\n'); lastNewline >= 0 {
			truncated = truncated[:lastNewline]
		}
		return truncated + "\n\n[Full release notes](" + url + ")"
	default:
		return text
	}
}
