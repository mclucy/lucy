import re
import html
import argparse

COLOR_PATTERN = re.compile(
    r'\[color=#([0-9a-fA-F]{6})\](.*?)\[/color\]',
    re.DOTALL
)


def hex_to_rgb(hex_color):
    r = int(hex_color[0:2], 16)
    g = int(hex_color[2:4], 16)
    b = int(hex_color[4:6], 16)
    return r, g, b


def convert_to_ansi(text):
    text = html.unescape(text)

    def repl(match):
        hex_color = match.group(1)
        content = match.group(2)

        if not content:
            return ""

        r, g, b = hex_to_rgb(hex_color)

        ansi_start = f"\033[38;2;{r};{g};{b}m"
        ansi_end = "\033[0m"

        return ansi_start + content + ansi_end

    return COLOR_PATTERN.sub(repl, text)


def strip_other_tags(text):
    text = re.sub(r'\[/?size.*?\]', '', text)
    text = re.sub(r'\[/?font.*?\]', '', text)
    text = re.sub(r'<.*?>', '', text)
    return text


def main():
    parser = argparse.ArgumentParser(
        description="Convert BBCode color text to ANSI escape codes")
    parser.add_argument("input", help="input file")

    args = parser.parse_args()

    with open(args.input, "r", encoding="utf-8") as f:
        data = f.read()

    data = strip_other_tags(data)
    ansi = convert_to_ansi(data)

    print(ansi, end="")


if __name__ == "__main__":
    main()
