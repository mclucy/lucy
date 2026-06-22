package cmd

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/mclucy/lucy/types"
)

// renderTopologyASCII renders a RuntimeTopology as an ASCII diagram.
// Nodes are drawn as boxes and edges as straight lines with centered labels,
// producing an undirected visual style such as --"modifies"--.
func renderTopologyASCII(
	topology *types.RuntimeTopology,
	direction string,
	noStyle bool,
	longOut bool,
) string {
	boxes := layoutTopologyBoxes(topology, direction, longOut)
	if len(boxes) == 0 {
		return ""
	}

	canvas := newTopologyCanvas()
	for i := range boxes {
		canvas.drawBox(&boxes[i], noStyle)
	}

	boxByID := make(map[types.RuntimeNodeID]*topologyBox, len(boxes))
	for i := range boxes {
		boxByID[boxes[i].node.ID] = &boxes[i]
	}

	for _, edge := range topology.Edges {
		from, okFrom := boxByID[edge.From]
		to, okTo := boxByID[edge.To]
		if !okFrom || !okTo {
			continue
		}
		canvas.drawEdge(from, to, string(edge.Verb), direction, noStyle)
	}

	return canvas.String()
}

type topologyBox struct {
	node   types.RuntimeNode
	label  string
	level  int
	x      int
	y      int
	width  int
	height int
}

func layoutTopologyBoxes(
	topology *types.RuntimeTopology,
	direction string,
	longOut bool,
) []topologyBox {
	levels := bfsLevels(topology)

	boxes := make([]topologyBox, 0, len(topology.Nodes))
	for _, node := range topology.Nodes {
		label := buildNodeLabel(node, topology.PrimaryNode, longOut)
		lines := strings.Split(label, "\\n")
		width := maxLineWidth(lines) + 4 // 2 borders + 2 padding
		height := len(lines) + 2         // 2 borders
		boxes = append(boxes, topologyBox{
			node:   node,
			label:  label,
			level:  levels[node.ID],
			width:  width,
			height: height,
		})
	}

	if direction == "LR" {
		placeBoxesLR(&boxes, topology, levels)
	} else {
		placeBoxesTD(&boxes, topology, levels)
	}

	return boxes
}

func bfsLevels(topology *types.RuntimeTopology) map[types.RuntimeNodeID]int {
	levels := make(map[types.RuntimeNodeID]int, len(topology.Nodes))
	for _, node := range topology.Nodes {
		levels[node.ID] = -1
	}

	if topology.PrimaryNode == "" {
		for _, node := range topology.Nodes {
			levels[node.ID] = 0
		}
		return levels
	}

	queue := []types.RuntimeNodeID{topology.PrimaryNode}
	levels[topology.PrimaryNode] = 0

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, edge := range topology.Edges {
			if edge.From != current {
				continue
			}
			if levels[edge.To] != -1 {
				continue
			}
			levels[edge.To] = levels[current] + 1
			queue = append(queue, edge.To)
		}
	}

	for _, node := range topology.Nodes {
		if levels[node.ID] == -1 {
			levels[node.ID] = 0
		}
	}

	return levels
}

func placeBoxesTD(boxes *[]topologyBox, topology *types.RuntimeTopology, levels map[types.RuntimeNodeID]int) {
	maxLevel := maxLevel(levels)
	levelBoxes := groupByLevel(*boxes)

	rowHeights := make([]int, maxLevel+1)
	for level, group := range levelBoxes {
		for _, b := range group {
			if b.height > rowHeights[level] {
				rowHeights[level] = b.height
			}
		}
	}

	rowSpacing := make([]int, maxLevel+1)
	const minRowSpacing = 4
	for level := 0; level <= maxLevel; level++ {
		rowSpacing[level] = minRowSpacing
	}

	rowY := make([]int, maxLevel+1)
	currentY := 0
	for level := 0; level <= maxLevel; level++ {
		rowY[level] = currentY
		currentY += rowHeights[level] + rowSpacing[level]
	}

	const horizontalSpacing = 2
	for level := 0; level <= maxLevel; level++ {
		group := levelBoxes[level]
		if len(group) == 0 {
			continue
		}

		totalWidth := 0
		for _, b := range group {
			totalWidth += b.width
		}
		totalWidth += (len(group) - 1) * horizontalSpacing

		currentX := 0
		for _, b := range group {
			b.x = currentX
			b.y = rowY[level] + (rowHeights[level]-b.height)/2
			currentX += b.width + horizontalSpacing
		}
	}
}

func placeBoxesLR(boxes *[]topologyBox, topology *types.RuntimeTopology, levels map[types.RuntimeNodeID]int) {
	maxLevel := maxLevel(levels)
	levelBoxes := groupByLevel(*boxes)

	colWidths := make([]int, maxLevel+1)
	for level, group := range levelBoxes {
		for _, b := range group {
			if b.width > colWidths[level] {
				colWidths[level] = b.width
			}
		}
	}

	// Horizontal space between columns must fit edge labels with dashes.
	colSpacing := make([]int, maxLevel+1)
	const minColSpacing = 8
	for _, edge := range topology.Edges {
		fromLevel := levels[edge.From]
		toLevel := levels[edge.To]
		if toLevel != fromLevel+1 {
			continue
		}
		needed := utf8.RuneCountInString(edgeLabel(edge.Verb)) + 5
		if needed > colSpacing[fromLevel] {
			colSpacing[fromLevel] = needed
		}
	}
	for level := 0; level <= maxLevel; level++ {
		if colSpacing[level] < minColSpacing {
			colSpacing[level] = minColSpacing
		}
	}

	colX := make([]int, maxLevel+1)
	currentX := 0
	for level := 0; level <= maxLevel; level++ {
		colX[level] = currentX
		currentX += colWidths[level] + colSpacing[level]
	}

	const verticalSpacing = 1
	for level := 0; level <= maxLevel; level++ {
		group := levelBoxes[level]
		if len(group) == 0 {
			continue
		}

		totalHeight := 0
		for _, b := range group {
			totalHeight += b.height
		}
		totalHeight += (len(group) - 1) * verticalSpacing

		currentY := 0
		for _, b := range group {
			b.x = colX[level] + (colWidths[level]-b.width)/2
			b.y = currentY
			currentY += b.height + verticalSpacing
		}
	}
}

func edgeLabel(verb types.RuntimeEdgeVerb) string {
	if verb == "" {
		return ""
	}
	return fmt.Sprintf("\"%s\"", verb)
}

func maxLevel(levels map[types.RuntimeNodeID]int) int {
	max := 0
	for _, level := range levels {
		if level > max {
			max = level
		}
	}
	return max
}

func groupByLevel(boxes []topologyBox) map[int][]*topologyBox {
	groups := make(map[int][]*topologyBox)
	for i := range boxes {
		level := boxes[i].level
		groups[level] = append(groups[level], &boxes[i])
	}
	return groups
}

func maxLineWidth(lines []string) int {
	maxW := 0
	for _, line := range lines {
		if w := utf8.RuneCountInString(line); w > maxW {
			maxW = w
		}
	}
	return maxW
}

type topologyCanvas struct {
	cells [][]rune
}

func newTopologyCanvas() *topologyCanvas {
	return &topologyCanvas{cells: [][]rune{}}
}

func (c *topologyCanvas) ensureSize(width, height int) {
	for len(c.cells) < height {
		c.cells = append(c.cells, make([]rune, 0, width))
	}
	for y := range c.cells {
		for len(c.cells[y]) < width {
			c.cells[y] = append(c.cells[y], ' ')
		}
	}
}

func (c *topologyCanvas) set(x, y int, r rune) {
	if x < 0 || y < 0 {
		return
	}
	c.ensureSize(x+1, y+1)
	c.cells[y][x] = r
}

func (c *topologyCanvas) drawText(x, y int, s string) {
	for i, r := range s {
		c.set(x+i, y, r)
	}
}

func (c *topologyCanvas) drawHLine(x1, x2, y int, ch rune) {
	if x1 > x2 {
		x1, x2 = x2, x1
	}
	for x := x1; x <= x2; x++ {
		c.set(x, y, ch)
	}
}

func (c *topologyCanvas) drawVLine(x, y1, y2 int, ch rune) {
	if y1 > y2 {
		y1, y2 = y2, y1
	}
	for y := y1; y <= y2; y++ {
		c.set(x, y, ch)
	}
}

func (c *topologyCanvas) drawBox(b *topologyBox, noStyle bool) {
	lines := strings.Split(b.label, "\\n")

	if noStyle {
		c.set(b.x, b.y, '+')
		c.drawHLine(b.x+1, b.x+b.width-2, b.y, '-')
		c.set(b.x+b.width-1, b.y, '+')
	} else {
		c.set(b.x, b.y, '┌')
		c.drawHLine(b.x+1, b.x+b.width-2, b.y, '─')
		c.set(b.x+b.width-1, b.y, '┐')
	}

	for i := 0; i < b.height-2; i++ {
		content := ""
		if i < len(lines) {
			content = lines[i]
		}
		pad := (b.width - 2 - utf8.RuneCountInString(content)) / 2
		if noStyle {
			c.set(b.x, b.y+1+i, '|')
			c.drawText(b.x+1+pad, b.y+1+i, content)
			c.set(b.x+b.width-1, b.y+1+i, '|')
		} else {
			c.set(b.x, b.y+1+i, '│')
			c.drawText(b.x+1+pad, b.y+1+i, content)
			c.set(b.x+b.width-1, b.y+1+i, '│')
		}
	}

	if noStyle {
		c.set(b.x, b.y+b.height-1, '+')
		c.drawHLine(b.x+1, b.x+b.width-2, b.y+b.height-1, '-')
		c.set(b.x+b.width-1, b.y+b.height-1, '+')
	} else {
		c.set(b.x, b.y+b.height-1, '└')
		c.drawHLine(b.x+1, b.x+b.width-2, b.y+b.height-1, '─')
		c.set(b.x+b.width-1, b.y+b.height-1, '┘')
	}
}

func (c *topologyCanvas) drawEdge(
	from, to *topologyBox,
	label string,
	direction string,
	noStyle bool,
) {
	if direction == "LR" {
		c.drawEdgeLR(from, to, label, noStyle)
	} else {
		c.drawEdgeTD(from, to, label, noStyle)
	}
}

func (c *topologyCanvas) drawEdgeLR(
	from, to *topologyBox,
	label string,
	noStyle bool,
) {
	y := from.y + from.height/2
	x1 := from.x + from.width
	x2 := to.x - 1

	lineChar := rune('─')
	arrowHead := rune('►')
	if noStyle {
		lineChar = '-'
		arrowHead = '>'
	}

	c.drawHLine(x1, x2-1, y, lineChar)
	c.set(x2, y, arrowHead)

	if label != "" {
		text := edgeLabel(types.RuntimeEdgeVerb(label))
		labelX := x1 + (x2-1-x1+1-utf8.RuneCountInString(text))/2
		c.drawText(labelX, y, text)
	}
}

func (c *topologyCanvas) drawEdgeTD(
	from, to *topologyBox,
	label string,
	noStyle bool,
) {
	x := from.x + from.width/2
	y1 := from.y + from.height
	y2 := to.y - 1

	lineChar := rune('│')
	arrowHead := rune('▼')
	if noStyle {
		lineChar = '|'
		arrowHead = 'v'
	}

	c.drawVLine(x, y1, y2-1, lineChar)
	c.set(x, y2, arrowHead)

	if label != "" {
		text := edgeLabel(types.RuntimeEdgeVerb(label))
		labelY := y1 + (y2-1-y1+1)/2
		labelX := x + 1
		c.drawText(labelX, labelY, text)
	}
}

func (c *topologyCanvas) String() string {
	var b strings.Builder
	for _, row := range c.cells {
		b.WriteString(strings.TrimRight(string(row), " "))
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}
