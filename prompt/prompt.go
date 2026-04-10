package prompt

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

func Note(title, description string) error {
	if title != "" {
		fmt.Println(title)
	}
	if description != "" {
		fmt.Println(description)
	}
	fmt.Print("Press Enter to continue...")
	_, err := readLine()
	if err != nil {
		return err
	}
	return nil
}

func Confirm(title, description, affirmative, negative string) (bool, error) {
	if affirmative == "" {
		affirmative = "yes"
	}
	if negative == "" {
		negative = "no"
	}

	if title != "" {
		fmt.Println(title)
	}
	if description != "" {
		fmt.Println(description)
	}

	for {
		fmt.Printf("[%s/%s]: ", affirmative, negative)
		line, err := readLine()
		if err != nil {
			return false, err
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes", strings.ToLower(affirmative):
			return true, nil
		case "n", "no", strings.ToLower(negative):
			return false, nil
		default:
			fmt.Println("Please answer yes or no.")
		}
	}
}

func Select[T any](title string, options []T, format func(T) string) (T, error) {
	var zero T
	if len(options) == 0 {
		return zero, fmt.Errorf("no options available")
	}

	if title != "" {
		fmt.Println(title)
	}
	for i, option := range options {
		fmt.Printf("%d) %s\n", i+1, format(option))
	}

	for {
		fmt.Printf("Select [1-%d]: ", len(options))
		line, err := readLine()
		if err != nil {
			return zero, err
		}
		choice, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil || choice < 1 || choice > len(options) {
			fmt.Println("Invalid selection.")
			continue
		}
		return options[choice-1], nil
	}
}

func readLine() (string, error) {
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		if err == io.EOF {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" {
				return trimmed, nil
			}
		}
		return "", err
	}
	return strings.TrimSpace(line), nil
}
