package main

import (
	"image"
	"image/color"
	"math"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
)

// Helper trig functions for pie drawing
func sin(a float64) float64 {
	return float64(math.Sin(a))
}
func cos(a float64) float64 {
	return float64(math.Cos(a))
}

// Helper to draw a line on an ebiten.Image (Bresenham's)
func drawLine(img *ebiten.Image, x0, y0, x1, y1 float64, clr color.Color) {
	dx := math.Abs(x1 - x0)
	dy := math.Abs(y1 - y0)
	x, y := int(x0+0.5), int(y0+0.5)
	xEnd, yEnd := int(x1+0.5), int(y1+0.5)
	sx := 1
	if x0 > x1 {
		sx = -1
	}
	sy := 1
	if y0 > y1 {
		sy = -1
	}
	if dx > dy {
		err := dx / 2
		for x != xEnd {
			img.Set(x, y, clr)
			err -= dy
			if err < 0 {
				y += sy
				err += dx
			}
			x += sx
		}
	} else {
		err := dy / 2
		for y != yEnd {
			img.Set(x, y, clr)

			err -= dx
			if err < 0 {
				x += sx
				err += dy
			}
			y += sy
		}
	}
	img.Set(xEnd, yEnd, clr)
}

// Helper: wrap text to fit inside inventory cell (maxCharsPerLine)
func wrapTextToCell(text string, maxChars int) []string {
	words := strings.Fields(text)
	var lines []string
	var current string
	for _, word := range words {
		if len(current)+len(word)+(func() int {
			if current != "" {
				return 1
			}
			return 0
		}()) > maxChars {
			lines = append(lines, current)
			current = word
		} else {
			if current != "" {
				current += " "
			}
			current += word
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func isFacingNPC(g *Game, npc *NPC) bool {
	// Player must be within a 2x2 tile area around the NPC (more generous)
	playerRect := image.Rect(g.playerPos.X, g.playerPos.Y, g.playerPos.X+tileSize, g.playerPos.Y+tileSize) // 4x greater
	npcRect := image.Rect(npc.pos.X-tileSize/2, npc.pos.Y-tileSize/2, npc.pos.X+tileSize*3/2, npc.pos.Y+tileSize*3/2)
	return playerRect.Overlaps(npcRect)
}

func sign(x int) int {
	if x < 0 {
		return -1
	}
	if x > 0 {
		return 1
	}
	return 0
}

func wrapText(text string, maxWidth int) []string {
	// Simple word wrap: splits text into lines not exceeding maxWidth (in runes)
	words := strings.Fields(text)
	var lines []string
	var current string
	for _, word := range words {
		if len([]rune(current))+len([]rune(word))+1 > maxWidth {
			lines = append(lines, current)
			current = word
		} else {
			if current != "" {
				current += " "
			}
			current += word
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
