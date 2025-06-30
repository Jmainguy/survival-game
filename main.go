package main

import (
	"image"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/audio"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/lafriks/go-tiled"
)

const (
	tileSize = 15 // Adjusted to match actual tile size
	scale    = 2  // Zoom in 2x
)

func main() {
	// Load map
	mapData, err := tiled.LoadFile("assets/jons_first_map.tmx")
	if err != nil {
		log.Fatal(err)
	}

	tilesetImgs := make(map[string]*ebiten.Image)
	for _, ts := range mapData.Tilesets {
		imgPath := ts.Image.Source
		if imgPath != "" && imgPath[0] != '/' {
			imgPath = "assets/" + imgPath
		}
		tileImg, _, err := ebitenutil.NewImageFromFile(imgPath)
		if err != nil {
			log.Printf("failed to load tileset image %s: %v", imgPath, err)
			continue
		}
		tilesetImgs[imgPath] = tileImg
	}

	// Load character idle and walk sprite sheets for Farmer
	idleSprite, _, err := ebitenutil.NewImageFromFile("assets/Farmer_idle.png")
	if err != nil {
		log.Printf("failed to load idle sprite: %v", err)
		idleSprite = nil
	}
	walkSprite, _, err := ebitenutil.NewImageFromFile("assets/Farmer_walk.png")
	if err != nil {
		log.Printf("failed to load walk sprite: %v", err)
		walkSprite = nil
	}

	// Play background music: gather all .mp3 files in assets/
	musicFiles := []string{}
	files, err := os.ReadDir("assets")
	if err == nil {
		for _, f := range files {
			if !f.IsDir() && strings.HasSuffix(strings.ToLower(f.Name()), ".mp3") {
				musicFiles = append(musicFiles, filepath.Join("assets", f.Name()))
			}
		}
	}
	rand.Shuffle(len(musicFiles), func(i, j int) { musicFiles[i], musicFiles[j] = musicFiles[j], musicFiles[i] })

	const sampleRate = 44100
	audioContext := audio.NewContext(sampleRate)

	// Calculate player starting position: center of map
	startX := (mapData.Width*tileSize - tileSize) / 2
	startY := (mapData.Height*tileSize - tileSize) / 2

	game := &Game{
		mapData:      mapData,
		playerPos:    image.Point{X: startX, Y: startY},
		tilesetImgs:  tilesetImgs,
		idleSprite:   idleSprite,
		walkSprite:   walkSprite,
		audioContext: audioContext,
		musicFiles:   musicFiles,
		musicPlayed:  []string{},
		health:       1.0,
		social:       1.0,
		hunger:       1.0,
		gameMinutes:  8 * 60, // Start at 08:00
	}
	game.spawnNPCs()
	// Set window size to half the scaled map size
	winW := game.mapData.Width * tileSize * scale / 2
	winH := game.mapData.Height * tileSize * scale / 2
	if winW > game.mapData.Width*tileSize*scale {
		winW = game.mapData.Width * tileSize * scale
	}
	if winH > game.mapData.Height*tileSize*scale {
		winH = game.mapData.Height * tileSize * scale
	}
	ebiten.SetWindowSize(winW, winH)
	ebiten.SetWindowTitle("First Game")

	if err := ebiten.RunGame(game); err != nil {
		log.Fatal(err)
	}
}
