package main

import (
	"image"
	"image/color"
	"log"
	"math"
	"math/rand"
	"os"
	"strconv"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/audio"
	"github.com/hajimehoshi/ebiten/v2/audio/mp3"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
)

func (g *Game) Update() error {
	if g.gameOver {
		// Restart game on any key press or mouse click
		if ebiten.IsKeyPressed(ebiten.KeySpace) || ebiten.IsKeyPressed(ebiten.KeyEnter) || ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) {
			g.restart()
		}
		return nil
	}

	// Find and cache all collidable layers except "Base ground"
	collidableLayers := map[string]bool{}
	for _, layer := range g.mapData.Layers {
		if layer.Name != "Base ground" && layer.Visible {
			collidableLayers[layer.Name] = true
		}
	}
	// Find the "Trees" layer if not already cached
	if g.treesLayer == nil {
		for _, layer := range g.mapData.Layers {
			if layer.Name == "Trees" {
				g.treesLayer = layer
				break
			}
		}
	}

	const moveSpeed = 1

	// --- NPC movement and animation logic ---
	for _, npc := range g.npcs {
		// If chatting and this is the chatting NPC, freeze movement
		if g.chatting && g.chatNPC == npc {
			npc.moving = false
			continue
		}
		// If chatting, freeze all NPCs
		if g.chatting {
			npc.moving = false
			continue
		}
		if !npc.moving {
			npc.moveTick++
			if npc.moveTick > 30+rand.Intn(30) {
				npc.moveTick = 0
				dirs := []struct{ dx, dy, dir int }{
					{0, -tileSize, 3}, // up
					{0, tileSize, 0},  // down
					{-tileSize, 0, 2}, // left
					{tileSize, 0, 1},  // right
				}
				rand.Shuffle(len(dirs), func(i, j int) { dirs[i], dirs[j] = dirs[j], dirs[i] })
				for _, d := range dirs {
					newX := npc.pos.X + d.dx
					newY := npc.pos.Y + d.dy
					// Check map bounds
					if newX < 0 || newY < 0 || newX > g.mapData.Width*tileSize-tileSize/4 || newY > g.mapData.Height*tileSize-tileSize/4 {
						continue
					}
					// Check collision with all collidable layers except "Base ground"
					blocked := false
					for _, layer := range g.mapData.Layers {
						if layer.Name == "Base ground" || !layer.Visible {
							continue
						}
						npcSize := tileSize
						npcOffset := (tileSize - npcSize) / 2
						centerX := newX + npcOffset + npcSize/2
						centerY := newY + npcOffset + npcSize/2
						tileX := centerX / tileSize
						tileY := centerY / tileSize
						if tileX >= 0 && tileX < g.mapData.Width && tileY >= 0 && tileY < g.mapData.Height {
							tile := layer.Tiles[tileY*g.mapData.Width+tileX]
							if tile != nil && tile.Tileset != nil {
								blocked = true
								break
							}
						}
					}
					// Check collision with all other characters (player and NPCs)
					npcRect := image.Rect(newX, newY, newX+tileSize, newY+tileSize)
					playerRect := image.Rect(g.playerPos.X, g.playerPos.Y, g.playerPos.X+tileSize, g.playerPos.Y+tileSize)
					if npcRect.Overlaps(playerRect) {
						blocked = true
					}
					for _, other := range g.npcs {
						if other == npc {
							continue
						}
						otherRect := image.Rect(other.pos.X, other.pos.Y, other.pos.X+tileSize, other.pos.Y+tileSize)
						if npcRect.Overlaps(otherRect) {
							blocked = true
							break
						}
					}
					if !blocked {
						npc.target = image.Point{X: newX, Y: newY}
						npc.dir = d.dir
						npc.moving = true
						break
					}
				}
			}
		} else {
			// Move smoothly toward target
			step := 2
			dx := npc.target.X - npc.pos.X
			dy := npc.target.Y - npc.pos.Y
			if dx != 0 {
				if abs(dx) < step {
					npc.pos.X = npc.target.X
				} else {
					npc.pos.X += step * sign(dx)
				}
			}
			if dy != 0 {
				if abs(dy) < step {
					npc.pos.Y = npc.target.Y
				} else {
					npc.pos.Y += step * sign(dy)
				}
			}
			if npc.pos == npc.target {
				npc.moving = false
			}
		}
		// Animate NPC sprite
		npc.animTick++
		if npc.animTick > 15 {
			npc.anim = (npc.anim + 1) % 4
			npc.animTick = 0
		}
	}

	// --- In-game time and bar drain logic ---
	const realSecondsPerGameMinute = 10.0 / 60.0 // 10 real min per game hour, so 10/60 per game min
	now := time.Now()
	if g.lastTick.IsZero() {
		g.lastTick = now
	}
	elapsed := now.Sub(g.lastTick).Seconds()
	gameMinAdvance := int(elapsed / realSecondsPerGameMinute)
	if gameMinAdvance > 0 {
		g.gameMinutes = (g.gameMinutes + gameMinAdvance) % (24 * 60)
		g.lastTick = g.lastTick.Add(time.Duration(float64(gameMinAdvance) * realSecondsPerGameMinute * float64(time.Second)))
	}

	// Drain 50% from hunger/social every 24 in-game hours, but smoothly
	// That is, every in-game minute, drain (0.5 / 1440) from each
	drainPerMinute := 0.5 / 1440.0
	if g.lastDrain == 0 {
		g.lastDrain = g.gameMinutes
	}
	for g.lastDrain != g.gameMinutes {
		g.hunger -= drainPerMinute
		g.social -= drainPerMinute
		if g.hunger < 0 {
			g.hunger = 0
		}
		if g.social < 0 {
			g.social = 0
		}
		g.lastDrain = (g.lastDrain + 1) % (24 * 60)
	}

	// --- NPC interaction logic ---
	if g.chatting {
		const chatInputDelay = 200 * time.Millisecond
		now := time.Now()
		// Always allow a "Goodbye" choice if there are no choices or all choices are terminal
		if g.convNode != nil && len(g.convNode.Choices) == 0 {
			g.convNode.Choices = []ConversationChoice{
				{Text: "Goodbye", Next: nil},
			}
			g.chatChoice = 0
		}
		if len(g.convNode.Choices) > 0 {
			// Only allow input if enough time has passed since last choice
			if ebiten.IsKeyPressed(ebiten.KeyArrowUp) && now.Sub(g.lastChoiceTime) > chatInputDelay {
				g.chatChoice--
				if g.chatChoice < 0 {
					g.chatChoice = len(g.convNode.Choices) - 1
				}
				g.lastChoiceTime = now
			}
			if ebiten.IsKeyPressed(ebiten.KeyArrowDown) && now.Sub(g.lastChoiceTime) > chatInputDelay {
				g.chatChoice++
				if g.chatChoice >= len(g.convNode.Choices) {
					g.chatChoice = 0
				}
				g.lastChoiceTime = now
			}
			if ebiten.IsKeyPressed(ebiten.KeySpace) && now.Sub(g.lastChoiceTime) > chatInputDelay {
				choice := g.convNode.Choices[g.chatChoice]
				// --- Tree removal logic: remove tree tile immediately after "Okay" is chosen ---
				if g.pendingTreeLayer != nil && g.convNode.Text == "You cut down the tree." && choice.Text == "Okay" {
					g.pendingTreeLayer.Tiles[g.pendingTreeTileIdx] = nil // Remove tree tile from layer
					g.pendingTreeLayer = nil
					g.pendingTreeTileIdx = 0
					g.addToInventory("Wood", 10)
				}
				// --- Fishing logic: add fish if caught ---
				if g.convNode.Text == "You cast your line... (Nothing bites yet!)" && choice.Text == "Okay" {
					// If you want to only add fish when something is caught, change the text above
					// For now, add fish for demonstration
					g.addToInventory("Fish", 1)
				}
				if choice.Text == "Goodbye" || choice.Next == nil {
					g.chatting = false
					g.chatNPC = nil
					g.convNode = nil
					g.lastChatEnd = now
					// Social bar +10% when talking to NPC
					g.social += 0.10
					if g.social > 1.0 {
						g.social = 1.0
					}
				} else if choice.Next != nil {
					g.convNode = choice.Next
					g.chatChoice = 0
				}
				g.lastChoiceTime = now
			}
		}
		return nil // Don't allow movement while chatting
	}

	const inventoryInputDelay = 200 * time.Millisecond
	const inventoryBlockAfterAction = time.Second

	now = time.Now()

	// Prevent inventory open for 1s after tree cut or fishing
	blockInventory := false
	if now.Sub(g.lastChatEnd) < inventoryBlockAfterAction {
		blockInventory = true
	}

	// --- Inventory open/close logic with delay and block after action ---
	if ebiten.IsKeyPressed(ebiten.KeySpace) && !g.inventoryOpen && now.Sub(g.lastInventoryTime) > inventoryInputDelay && !blockInventory {
		// Only open inventory if not interacting with NPC, water, trees, or doors
		playerSize := tileSize
		playerOffset := (tileSize - playerSize) / 2
		centerX := g.playerPos.X + playerOffset + playerSize/2
		centerY := g.playerPos.Y + playerOffset + playerSize/2
		tileX := centerX / tileSize
		tileY := centerY / tileSize
		interactX, interactY := tileX, tileY
		switch g.playerDir {
		case 0:
			interactY++
		case 1:
			interactX--
		case 2:
			interactX++
		case 3:
			interactY--
		}
		for _, npc := range g.npcs {
			if isFacingNPC(g, npc) {
				goto skipInventoryOpen
			}
		}
		for _, layer := range g.mapData.Layers {
			if (layer.Name == "Water" || layer.Name == "Trees" || layer.Name == "Doors") &&
				interactX >= 0 && interactX < g.mapData.Width && interactY >= 0 && interactY < g.mapData.Height {
				tile := layer.Tiles[interactY*g.mapData.Width+interactX]
				if tile != nil && tile.Tileset != nil {
					goto skipInventoryOpen
				}
			}
		}
		g.inventoryOpen = true
		g.lastInventoryTime = now
		return nil
	}
skipInventoryOpen:

	// Inventory interaction: close with space, or cook/eat fish
	if g.inventoryOpen {
		// Only allow input if enough time has passed since last inventory action
		if ebiten.IsKeyPressed(ebiten.KeySpace) && now.Sub(g.lastInventoryTime) > inventoryInputDelay {
			g.inventoryOpen = false
			g.lastInventoryTime = now
			return nil
		}
		// Cook fish: press 'C'
		if ebiten.IsKeyPressed(ebiten.KeyC) && now.Sub(g.lastInventoryTime) > inventoryInputDelay {
			if g.hasItem("Fish", 1) && g.hasItem("Wood", 1) {
				g.removeItem("Fish", 1)
				g.removeItem("Wood", 1)
				g.addToInventory("Cooked Fish", 1)
				// Block inventory open for 1s after cooking
				g.lastChatEnd = now
			}
			g.lastInventoryTime = now
			return nil
		}
		// Eat cooked fish: press 'E'
		if ebiten.IsKeyPressed(ebiten.KeyE) && now.Sub(g.lastInventoryTime) > inventoryInputDelay {
			if g.hasItem("Cooked Fish", 1) {
				g.removeItem("Cooked Fish", 1)
				// Add 10% to hunger bar
				g.hunger += 0.10
				if g.hunger > 1.0 {
					g.hunger = 1.0
				}
			}
			g.lastInventoryTime = now
			return nil
		}
		return nil
	}

	// Player movement logic
	if !g.chatting {
		newPos := g.playerPos
		if ebiten.IsKeyPressed(ebiten.KeyArrowLeft) {
			newPos.X -= moveSpeed
			g.playerDir = 1 // left
			g.moving = true
		}
		if ebiten.IsKeyPressed(ebiten.KeyArrowRight) {
			newPos.X += moveSpeed
			g.playerDir = 2 // right
			g.moving = true
		}
		if ebiten.IsKeyPressed(ebiten.KeyArrowUp) {
			newPos.Y -= moveSpeed
			g.playerDir = 3 // up
			g.moving = true
		}
		if ebiten.IsKeyPressed(ebiten.KeyArrowDown) {
			newPos.Y += moveSpeed
			g.playerDir = 0 // down
			g.moving = true
		}

		// Clamp intended position to map bounds
		maxX := g.mapData.Width*tileSize - tileSize
		maxY := g.mapData.Height*tileSize - tileSize
		if newPos.X < 0 {
			newPos.X = 0
		}
		if newPos.Y < 0 {
			newPos.Y = 0
		}
		if newPos.X > maxX {
			newPos.X = maxX
		}
		if newPos.Y > maxY {
			newPos.Y = maxY
		}

		// Collision check with all collidable layers except "Base ground"
		blocked := false
		for _, layer := range g.mapData.Layers {
			if !collidableLayers[layer.Name] {
				continue
			}
			playerSize := tileSize
			playerOffset := (tileSize - playerSize) / 2
			centerX := newPos.X + playerOffset + playerSize/2
			centerY := newPos.Y + playerOffset + playerSize/2
			tileX := centerX / tileSize
			tileY := centerY / tileSize
			if tileX >= 0 && tileX < g.mapData.Width && tileY >= 0 && tileY < g.mapData.Height {
				tile := layer.Tiles[tileY*g.mapData.Width+tileX]
				if tile != nil && tile.Tileset != nil {
					blocked = true
					break
				}
			}
		}
		// Collision with all characters (player can't walk through NPCs or other players)
		playerRect := image.Rect(newPos.X, newPos.Y, newPos.X+tileSize, newPos.Y+tileSize)
		for _, npc := range g.npcs {
			npcRect := image.Rect(npc.pos.X, npc.pos.Y, npc.pos.X+tileSize, npc.pos.Y+tileSize)
			if playerRect.Overlaps(npcRect) {
				blocked = true
				break
			}
		}

		if !blocked {
			g.playerPos = newPos
		}

		// Animation: advance frame if moving, else reset to stand
		if g.moving {
			g.playerAnimTick++
			if g.playerAnimTick > 10 {
				g.playerAnim = (g.playerAnim + 1) % 4 // 4 frames: 0-3
				g.playerAnimTick = 0
			}
		} else {
			g.playerAnim = 0
			g.playerAnimTick = 0
		}

		// Check for NPC or layer interaction
		if ebiten.IsKeyPressed(ebiten.KeySpace) && !g.chatting {
			// Prevent chat if less than 1 second since last chat ended
			if time.Since(g.lastChatEnd) < time.Second {
				return nil
			}
			// --- NPC interaction ---
			for _, npc := range g.npcs {
				if isFacingNPC(g, npc) {
					g.lastChoiceTime = time.Now()
					g.chatting = true
					g.chatNPC = npc
					g.chatChoice = 0
					// Face each other
					if g.playerPos.X < npc.pos.X {
						g.playerDir = 2 // right
						npc.dir = 1     // npc faces left
					} else if g.playerPos.X > npc.pos.X {
						g.playerDir = 1 // left
						npc.dir = 2     // npc faces right
					} else if g.playerPos.Y < npc.pos.Y {
						g.playerDir = 0 // down
						npc.dir = 3     // npc faces up
					} else if g.playerPos.Y > npc.pos.Y {
						g.playerDir = 3 // up
						npc.dir = 0     // npc faces down
					}
					// Start conversation
					if g.conversations == nil {
						g.initConversations()
					}
					g.convNode = g.conversations[npc.name]
					return nil
				}
			}
			// --- Layer interaction (Water/Trees) ---
			playerSize := tileSize
			playerOffset := (tileSize - playerSize) / 2
			centerX := g.playerPos.X + playerOffset + playerSize/2
			centerY := g.playerPos.Y + playerOffset + playerSize/2
			tileX := centerX / tileSize
			tileY := centerY / tileSize

			// Always check all directions (not just up/down)
			interactX, interactY := tileX, tileY
			switch g.playerDir {
			case 0: // down
				interactY++
			case 1: // right
				interactX--
			case 2: // left
				interactX++
			case 3: // up
				interactY--
			}

			// Water interaction
			for _, layer := range g.mapData.Layers {
				if layer.Name == "Water" {
					if interactX >= 0 && interactX < g.mapData.Width && interactY >= 0 && interactY < g.mapData.Height {
						tile := layer.Tiles[interactY*g.mapData.Width+interactX]
						if tile != nil && tile.Tileset != nil {
							g.chatting = true
							g.chatNPC = nil
							g.chatChoice = 0
							g.convNode = &ConversationNode{
								Text: "You are at the water. Would you like to fish?",
								Choices: []ConversationChoice{
									{Text: "Yes, fish!", Next: &ConversationNode{
										Text: "You cast your line... (Nothing bites yet!)",
										Choices: []ConversationChoice{
											{Text: "Okay", Next: nil},
										},
									}},
									{Text: "No, walk away.", Next: nil},
								},
							}
							g.lastChoiceTime = time.Now()
							return nil
						}
					}
				}
			}
			// Tree interaction
			for _, layer := range g.mapData.Layers {
				if layer.Name == "Trees" {
					if interactX >= 0 && interactX < g.mapData.Width && interactY >= 0 && interactY < g.mapData.Height {
						tileIdx := interactY*g.mapData.Width + interactX
						tile := layer.Tiles[tileIdx]
						if tile != nil && tile.Tileset != nil {
							g.chatting = true
							g.chatNPC = nil
							g.chatChoice = 0
							g.convNode = &ConversationNode{
								Text: "You are facing a tree. Cut it down?",
								Choices: []ConversationChoice{
									{Text: "Yes, cut it down.", Next: &ConversationNode{
										Text: "You cut down the tree.",
										Choices: []ConversationChoice{
											{Text: "Okay", Next: nil},
										},
									}},
									{Text: "No, leave it.", Next: nil},
								},
							}
							g.lastChoiceTime = time.Now()
							g.pendingTreeLayer = layer
							g.pendingTreeTileIdx = tileIdx
							return nil
						}
					}
				}
			}
		}
	}

	// Handle music playback: if not playing, play next random mp3
	if g.musicPlayer == nil || !g.musicPlayer.IsPlaying() {
		next := nextMusicFile(&g.musicFiles, &g.musicPlayed)
		if next != "" && g.audioContext != nil {
			// Close previous player if exists
			if g.musicPlayer != nil {
				g.musicPlayer.Close()
				g.musicPlayer = nil
			}
			// Open file for the entire duration of playback
			f, err := os.Open(next)
			if err == nil {
				stream, err := mp3.DecodeWithSampleRate(44100, f)
				if err == nil {
					player, err := g.audioContext.NewPlayer(stream)
					if err == nil {
						player.SetVolume(0.5)
						player.Play()
						g.musicPlayer = player
						// Keep file open for the duration of playback
						go func(p *audio.Player, file *os.File) {
							for p.IsPlaying() {
								// Sleep a bit, then check again
								time.Sleep(100 * time.Millisecond)
							}
							file.Close()
						}(player, f)
					} else {
						f.Close()
					}
				} else {
					f.Close()
				}
			}
		}
	}

	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	// Camera: center on player, clamp to map bounds
	viewportW := g.mapData.Width * tileSize * scale / 2
	viewportH := g.mapData.Height * tileSize * scale / 2
	if viewportW > g.mapData.Width*tileSize*scale {
		viewportW = g.mapData.Width * tileSize * scale
	}
	if viewportH > g.mapData.Height*tileSize*scale {
		viewportH = g.mapData.Height * tileSize * scale
	}
	camX := g.playerPos.X*scale + (tileSize*scale)/2 - viewportW/2
	camY := g.playerPos.Y*scale + (tileSize*scale)/2 - viewportH/2
	maxCamX := g.mapData.Width*tileSize*scale - viewportW
	maxCamY := g.mapData.Height*tileSize*scale - viewportH
	if camX < 0 {
		camX = 0
	}
	if camY < 0 {
		camY = 0
	}
	if camX > maxCamX {
		camX = maxCamX
	}
	if camY > maxCamY {
		camY = maxCamY
	}

	// Draw all visible map layers, scaled up, with camera offset
	for _, layer := range g.mapData.Layers {
		if !layer.Visible {
			continue
		}
		for y := 0; y < g.mapData.Height; y++ {
			for x := 0; x < g.mapData.Width; x++ {
				tile := layer.Tiles[y*g.mapData.Width+x]
				if tile == nil || tile.Tileset == nil {
					continue
				}
				ts := tile.Tileset
				imgPath := ts.Image.Source
				if imgPath != "" && imgPath[0] != '/' {
					imgPath = "assets/" + imgPath
				}
				tileImg := g.tilesetImgs[imgPath]
				if tileImg == nil {
					continue
				}
				tilesPerRow := (ts.Image.Width - ts.Margin*2 + ts.Spacing) / (ts.TileWidth + ts.Spacing)
				tileID := int(tile.ID)
				tileX := ts.Margin + (tileID%tilesPerRow)*(ts.TileWidth+ts.Spacing)
				tileY := ts.Margin + (tileID/tilesPerRow)*(ts.TileHeight+ts.Spacing)
				src := image.Rect(tileX, tileY, tileX+ts.TileWidth, tileY+ts.TileHeight)
				op := &ebiten.DrawImageOptions{}
				op.GeoM.Scale(scale, scale)
				op.GeoM.Translate(float64(x*tileSize*scale-camX), float64(y*tileSize*scale-camY))
				screen.DrawImage(tileImg.SubImage(src).(*ebiten.Image), op)
			}
		}
	}
	// Draw player using idle/walk sprite sheet if loaded
	spriteW, spriteH := 32, 48 // Each frame is 32x48 pixels for 128x192 sheets (4x4)
	var spriteSheet *ebiten.Image
	if g.moving && g.walkSprite != nil {
		spriteSheet = g.walkSprite
	} else if g.idleSprite != nil {
		spriteSheet = g.idleSprite
	}
	if spriteSheet != nil {
		sx := g.playerAnim * spriteW
		// Map playerDir to correct row in sprite sheet:
		// 0=down (row 0), 1=right (row 2), 2=left (row 1), 3=up (row 3)
		var animRow int
		switch g.playerDir {
		case 0: // down
			animRow = 0
		case 1: // right
			animRow = 1
		case 2: // left
			animRow = 2
		case 3: // up
			animRow = 3
		}
		sy := animRow * spriteH
		src := image.Rect(sx, sy, sx+spriteW, sy+spriteH)

		playerSize := tileSize
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Scale(
			float64(playerSize)/float64(spriteW)*scale,
			float64(playerSize)/float64(spriteH)*scale,
		)
		op.GeoM.Translate(float64(g.playerPos.X*scale-camX), float64(g.playerPos.Y*scale-camY))
		screen.DrawImage(spriteSheet.SubImage(src).(*ebiten.Image), op)
	} else {
		// fallback: red square
		playerSize := tileSize / 4
		playerImg := ebiten.NewImage(playerSize, playerSize)
		playerImg.Fill(color.RGBA{255, 0, 0, 255})
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Scale(scale, scale)
		op.GeoM.Translate(float64(g.playerPos.X*scale-camX), float64(g.playerPos.Y*scale-camY))
		screen.DrawImage(playerImg, op)
	}

	// Draw NPCs with animation (use same logic as enemies)
	spriteW, spriteH = 32, 48
	for _, npc := range g.npcs {
		if npc.sprite != nil {
			sx := npc.anim * spriteW
			var animRow int
			switch npc.dir {
			case 0: // down
				animRow = 0
			case 1: // right
				animRow = 2 // swap right to left row
			case 2: // left
				animRow = 1 // swap left to right row
			case 3: // up
				animRow = 3
			}
			sy := animRow * spriteH
			src := image.Rect(sx, sy, sx+spriteW, sy+spriteH)
			op := &ebiten.DrawImageOptions{}
			op.GeoM.Scale(float64(tileSize)/float64(spriteW)*scale, float64(tileSize)/float64(spriteH)*scale)
			op.GeoM.Translate(float64(npc.pos.X*scale-camX), float64(npc.pos.Y*scale-camY))
			screen.DrawImage(npc.sprite.SubImage(src).(*ebiten.Image), op)
		}
	}

	// Draw status bars and clock at top left as circular pies
	barRadius := 38
	barPad := 18
	x := 60
	y := 60

	// Helper to draw a pie/circle bar
	drawPie := func(centerX, centerY int, radius int, percent float64, bg, fg color.Color, label string) {
		img := ebiten.NewImage(radius*2, radius*2)
		// Draw background circle
		for dy := -radius; dy <= radius; dy++ {
			for dx := -radius; dx <= radius; dx++ {
				if dx*dx+dy*dy <= radius*radius {
					img.Set(radius+dx, radius+dy, bg)
				}
			}
		}
		// Draw filled arc for percent
		angleMax := int(percent * 360)
		for a := 0; a < angleMax; a++ {
			rad := float64(a) * (3.14159265 / 180)
			for r := 0; r < radius; r++ {
				px := radius + int(float64(r)*cos(rad))
				py := radius + int(float64(r)*sin(rad))
				if px >= 0 && px < radius*2 && py >= 0 && py < radius*2 {
					img.Set(px, py, fg)
				}
			}
		}
		// Draw label in center
		ebitenutil.DebugPrintAt(img, label, radius-len(label)*3, radius-6)
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Translate(float64(centerX-radius), float64(centerY-radius))
		screen.DrawImage(img, op)
	}

	// Health Pie
	healthVal := g.health
	if healthVal < 0 {
		healthVal = 0
	}
	if healthVal > 1 {
		healthVal = 1
	}
	drawPie(x, y, barRadius, healthVal, color.RGBA{60, 0, 0, 255}, color.RGBA{200, 0, 0, 255}, "Health")

	// Social Pie
	socialVal := g.social
	if socialVal < 0 {
		socialVal = 0
	}
	if socialVal > 1 {
		socialVal = 1
	}
	drawPie(x+barRadius*2+barPad, y, barRadius, socialVal, color.RGBA{0, 0, 60, 255}, color.RGBA{0, 0, 200, 255}, "Social")

	// Hunger Pie
	hungerVal := g.hunger
	if hungerVal < 0 {
		hungerVal = 0
	}
	if hungerVal > 1 {
		hungerVal = 1
	}
	drawPie(x+2*(barRadius*2+barPad), y, barRadius, hungerVal, color.RGBA{60, 40, 0, 255}, color.RGBA{200, 160, 0, 255}, "Hunger")

	// Draw clock as a pie/circle with two hands (hour and minute), no numbers
	clockX := x + 3*(barRadius*2+barPad)
	clockY := y
	clockRadius := barRadius
	clockImg := ebiten.NewImage(clockRadius*2, clockRadius*2)
	// Draw clock face
	for dy := -clockRadius; dy <= clockRadius; dy++ {
		for dx := -clockRadius; dx <= clockRadius; dx++ {
			if dx*dx+dy*dy <= clockRadius*clockRadius {
				clockImg.Set(clockRadius+dx, clockRadius+dy, color.RGBA{30, 30, 30, 220})
			}
		}
	}
	// Draw hour and minute hands
	gameHour := g.gameMinutes / 60
	gameMin := g.gameMinutes % 60
	// Minute hand (longer)
	minAngle := 2 * math.Pi * (float64(gameMin) / 60.0)
	minLen := float64(clockRadius) * 0.85
	mx := float64(clockRadius) + minLen*math.Sin(minAngle)
	my := float64(clockRadius) - minLen*math.Cos(minAngle)
	drawLine(clockImg, float64(clockRadius), float64(clockRadius), mx, my, color.RGBA{220, 220, 220, 255})
	// Hour hand (shorter), color changes for AM/PM
	hourAngle := 2 * math.Pi * ((float64(gameHour%12) + float64(gameMin)/60.0) / 12.0)
	hourLen := float64(clockRadius) * 0.55
	hx := float64(clockRadius) + hourLen*math.Sin(hourAngle)
	hy := float64(clockRadius) - hourLen*math.Cos(hourAngle)
	var hourColor color.RGBA
	if gameHour < 12 {
		// AM: blueish
		hourColor = color.RGBA{80, 180, 255, 255}
	} else {
		// PM: orange/red
		hourColor = color.RGBA{255, 140, 60, 255}
	}
	drawLine(clockImg, float64(clockRadius), float64(clockRadius), hx, hy, hourColor)
	for a := 0; a < 360; a++ {
		rad := float64(a) * (math.Pi / 180)
		px := clockRadius + int(float64(clockRadius)*math.Cos(rad))
		py := clockRadius + int(float64(clockRadius)*math.Sin(rad))
		if px >= 0 && px < clockRadius*2 && py >= 0 && py < clockRadius*2 {
			clockImg.Set(px, py, color.RGBA{80, 80, 80, 255})
		}
	}
	opClock := &ebiten.DrawImageOptions{}
	opClock.GeoM.Translate(float64(clockX-clockRadius), float64(clockY-clockRadius))
	screen.DrawImage(clockImg, opClock)

	// Gradual darken/brighten screen based on time of day
	// Dawn: 5:00-8:00, Dusk: 18:00-21:00, Night: 21:00-5:00, Day: 8:00-18:00
	// Alpha: 0 (brightest) to 160 (darkest)
	hour := float64(gameHour) + float64(gameMin)/60.0
	var overlayAlpha uint8
	if hour >= 5 && hour < 8 {
		// Dawn: fade from dark to bright
		// 5:00 = 160, 8:00 = 0
		overlayAlpha = uint8(160 - 160*(hour-5)/3)
	} else if hour >= 8 && hour < 18 {
		// Day: brightest
		overlayAlpha = 0
	} else if hour >= 18 && hour < 21 {
		// Dusk: fade from bright to dark
		// 18:00 = 0, 21:00 = 160
		overlayAlpha = uint8(160 * (hour - 18) / 3)
	} else {
		// Night: darkest
		overlayAlpha = 160
	}
	if overlayAlpha > 0 {
		w, h := screen.Bounds().Dx(), screen.Bounds().Dy()
		overlay := ebiten.NewImage(w, h)
		overlay.Fill(color.RGBA{0, 0, 0, overlayAlpha})
		screen.DrawImage(overlay, nil)
	}

	// Draw chat window if chatting (including fishing/tree dialogues)
	if g.chatting && g.convNode != nil {
		w, h := screen.Bounds().Dx(), screen.Bounds().Dy()
		textLines := wrapText(g.convNode.Text, 38)
		wrappedChoices := make([][]string, len(g.convNode.Choices))
		totalChoiceLines := 0
		for i, choice := range g.convNode.Choices {
			wrapped := wrapText(choice.Text, 38)
			wrappedChoices[i] = wrapped
			totalChoiceLines += len(wrapped)
		}
		winW := 320
		winH := 30 + len(textLines)*16 + 10 + totalChoiceLines*20 + 20
		if winH < 140 {
			winH = 140
		}
		x, y := (w-winW)/2, h-winH-20
		winImg := ebiten.NewImage(winW, winH)
		winImg.Fill(color.RGBA{30, 30, 30, 230})
		// Show NPC name if present, else show "Action"
		label := "Action:"
		if g.chatNPC != nil {
			label = g.chatNPC.name + ":"
		}
		ebitenutil.DebugPrintAt(winImg, label, 10, 10)
		for i, line := range textLines {
			ebitenutil.DebugPrintAt(winImg, line, 10, 30+i*16)
		}
		choiceY := 30 + len(textLines)*16 + 10
		lineIdx := 0
		for i, lines := range wrappedChoices {
			for j, line := range lines {
				prefix := "  "
				if i == g.chatChoice && j == 0 {
					prefix = "> "
				}
				ebitenutil.DebugPrintAt(winImg, prefix+line, 10, choiceY+lineIdx*20)
				prefix = "  "
				lineIdx++
			}
		}
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Translate(float64(x), float64(y))
		screen.DrawImage(winImg, op)
	}

	// Draw inventory if open
	if g.inventoryOpen {
		w, h := screen.Bounds().Dx(), screen.Bounds().Dy()
		invW := 400
		invH := 400
		actionW := 260
		actionH := invH
		x, y := (w-invW-actionW)/2+actionW, (h-invH)/2
		actionX, actionY := x-actionW, y
		invImg := ebiten.NewImage(invW, invH)
		invImg.Fill(color.RGBA{40, 40, 40, 240})
		ebitenutil.DebugPrintAt(invImg, "Inventory", 10, 10)
		cellW := 44
		cellH := 44
		for row := 0; row < 8; row++ {
			for col := 0; col < 8; col++ {
				slot := g.inventory[row][col]
				cellX := 10 + col*cellW
				cellY := 40 + row*cellH
				// Draw cell border
				for i := 0; i < cellW; i++ {
					invImg.Set(cellX+i, cellY, color.RGBA{80, 80, 80, 255})
					invImg.Set(cellX+i, cellY+cellH-1, color.RGBA{80, 80, 80, 255})
				}
				for i := 0; i < cellH; i++ {
					invImg.Set(cellX, cellY+i, color.RGBA{80, 80, 80, 255})
					invImg.Set(cellX+cellW-1, cellY+i, color.RGBA{80, 80, 80, 255})
				}
				// Draw item name and count, wrapped to cell width, count below name
				if slot.Item != "" && slot.Count > 0 {
					nameLines := wrapTextToCell(slot.Item, 7)
					for i, line := range nameLines {
						ebitenutil.DebugPrintAt(invImg, line, cellX+4, cellY+6+i*12)
					}
					countStr := "x" + strconv.Itoa(slot.Count)
					ebitenutil.DebugPrintAt(invImg, countStr, cellX+4, cellY+6+len(nameLines)*12)
				}
			}
		}
		// Draw actions in a separate window to the left of inventory
		actionImg := ebiten.NewImage(actionW, actionH)
		actionImg.Fill(color.RGBA{30, 30, 30, 240})
		ebitenutil.DebugPrintAt(actionImg, "Inventory Actions", 10, 10)
		// Wrap action lines to fit action window (max 32 chars per line)
		actionLines := wrapTextToCell("[C] Cook & Eat Fish (uses 1 Fish + 1 Wood)", 32)
		for i, line := range actionLines {
			ebitenutil.DebugPrintAt(actionImg, line, 10, 40+i*16)
		}
		eatLines := wrapTextToCell("[E] Eat Cooked Fish (uses 1 Cooked Fish)", 32)
		for i, line := range eatLines {
			ebitenutil.DebugPrintAt(actionImg, line, 10, 70+i*16)
		}
		closeLines := wrapTextToCell("[Space] Close", 32)
		for i, line := range closeLines {
			ebitenutil.DebugPrintAt(actionImg, line, 10, 110+i*16)
		}
		// Optionally, show a message if not enough resources
		if !g.hasItem("Fish", 1) || !g.hasItem("Wood", 1) {
			warnLines := wrapTextToCell("Need 1 Fish and 1 Wood to cook!", 32)
			for i, line := range warnLines {
				ebitenutil.DebugPrintAt(actionImg, line, 10, 150+i*16)
			}
		}
		opAction := &ebiten.DrawImageOptions{}
		opAction.GeoM.Translate(float64(actionX), float64(actionY))
		screen.DrawImage(actionImg, opAction)
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Translate(float64(x), float64(y))
		screen.DrawImage(invImg, op)
		return // Don't draw rest of game when inventory is open
	}

	// Draw game over overlay
	if g.gameOver {
		w, h := screen.Bounds().Dx(), screen.Bounds().Dy()
		overlay := ebiten.NewImage(w, h)
		overlay.Fill(color.RGBA{0, 0, 0, 180})
		screen.DrawImage(overlay, nil)
		ebitenutil.DebugPrintAt(screen, "GAME OVER\nPress Space/Enter/Mouse to Restart", w/2-80, h/2-10)
	}
}

func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	// Return viewport size (half the scaled map size)
	w := g.mapData.Width * tileSize * scale / 2
	h := g.mapData.Height * tileSize * scale / 2
	if w > g.mapData.Width*tileSize*scale {
		w = g.mapData.Width * tileSize * scale
	}
	if h > g.mapData.Height*tileSize*scale {
		h = g.mapData.Height * tileSize * scale
	}
	return w, h
}

// Add a restart method to reset the game state
func (g *Game) restart() {
	// Center player
	startX := (g.mapData.Width*tileSize - tileSize) / 2
	startY := (g.mapData.Height*tileSize - tileSize) / 2
	g.playerPos = image.Point{X: startX, Y: startY}
	g.playerAnim = 0
	g.playerAnimTick = 0
	g.moving = false
	g.gameOver = false
}

func (g *Game) spawnNPCs() {
	g.npcs = nil
	// Place NPCs at fixed locations for demo
	positions := []struct {
		name   string
		sprite string
		x, y   int
	}{
		{"Kid", "assets/Kid01_idle.png", 2 * tileSize, 2 * tileSize},
		{"Merchant", "assets/Merchant_idle.png", 5 * tileSize, 5 * tileSize},
		{"Alchemist", "assets/Alchemist_idle.png", 8 * tileSize, 8 * tileSize},
	}
	for _, p := range positions {
		img, _, err := ebitenutil.NewImageFromFile(p.sprite)
		if err != nil {
			log.Printf("failed to load NPC sprite %s: %v", p.sprite, err)
			continue
		}
		g.npcs = append(g.npcs, &NPC{
			pos:    image.Point{X: p.x, Y: p.y},
			dir:    0,
			name:   p.name,
			sprite: img,
		})
	}
}

func (g *Game) initConversations() {
	g.conversations = map[string]*ConversationNode{}

	// Kid conversation
	var kidRoot *ConversationNode
	kidEnd := &ConversationNode{
		Text: "See you later!",
		Choices: []ConversationChoice{
			{Text: "Goodbye", Next: nil},
			{Text: "How interesting, I wanted to ask you about something else...", Next: nil}, // placeholder, set below
		},
	}
	kidJoke := &ConversationNode{
		Text: "Why did the chicken cross the playground? To get to the other slide!",
		Choices: []ConversationChoice{
			{Text: "Haha! Got any more?", Next: kidEnd},
			{Text: "That's silly.", Next: kidEnd},
			{Text: "How interesting, I wanted to ask you about something else...", Next: nil}, // set below
			{Text: "Goodbye", Next: nil},
		},
	}
	kidFav := &ConversationNode{
		Text: "I love playing tag! What's your favorite game?",
		Choices: []ConversationChoice{
			{Text: "Hide and seek!", Next: kidEnd},
			{Text: "Chess.", Next: kidEnd},
			{Text: "How interesting, I wanted to ask you about something else...", Next: nil}, // set below
			{Text: "Goodbye", Next: nil},
		},
	}
	kidHowAreYou := &ConversationNode{
		Text: "I'm great! It's a fun day.",
		Choices: []ConversationChoice{
			{Text: "Glad to hear!", Next: kidEnd},
			{Text: "Tell me a joke!", Next: kidJoke},
			{Text: "How interesting, I wanted to ask you about something else...", Next: nil}, // set below
			{Text: "Goodbye", Next: nil},
		},
	}
	kidRoot = &ConversationNode{
		Text: "Hi! I'm the Kid. What do you want to talk about?",
		Choices: []ConversationChoice{
			{Text: "Tell me a joke!", Next: kidJoke},
			{Text: "What's your favorite game?", Next: kidFav},
			{Text: "How are you?", Next: kidHowAreYou},
			{Text: "Goodbye", Next: nil},
		},
	}
	// Set "How interesting..." choices to point to root
	kidEnd.Choices[1].Next = kidRoot
	kidJoke.Choices[2].Next = kidRoot
	kidFav.Choices[2].Next = kidRoot
	kidHowAreYou.Choices[2].Next = kidRoot
	g.conversations["Kid"] = kidRoot

	// Merchant conversation
	var merchantRoot *ConversationNode
	merchantEnd := &ConversationNode{
		Text: "Safe travels, friend!",
		Choices: []ConversationChoice{
			{Text: "Goodbye", Next: nil},
			{Text: "How interesting, I wanted to ask you about something else...", Next: nil}, // set below
		},
	}
	merchantTrade := &ConversationNode{
		Text: "Sorry, my shop is closed today.",
		Choices: []ConversationChoice{
			{Text: "Oh, that's too bad.", Next: merchantEnd},
			{Text: "What do you sell?", Next: &ConversationNode{
				Text: "Mostly potions and trinkets.",
				Choices: []ConversationChoice{
					{Text: "Sounds interesting!", Next: merchantEnd},
					{Text: "Can I see your wares?", Next: nil},                                        // Prevent infinite loop
					{Text: "How interesting, I wanted to ask you about something else...", Next: nil}, // set below
					{Text: "Goodbye", Next: nil},
				},
			}},
			{Text: "How interesting, I wanted to ask you about something else...", Next: nil}, // set below
			{Text: "Goodbye", Next: nil},
		},
	}
	merchantAnyNews := &ConversationNode{
		Text: "The harvest festival is coming soon.",
		Choices: []ConversationChoice{
			{Text: "Will there be games?", Next: merchantEnd},
			{Text: "Will you have a booth?", Next: merchantEnd},
			{Text: "How interesting, I wanted to ask you about something else...", Next: nil}, // set below
			{Text: "Goodbye", Next: nil},
		},
	}
	merchantWhereFrom := &ConversationNode{
		Text: "From the city to the east.",
		Choices: []ConversationChoice{
			{Text: "Do you miss it?", Next: merchantEnd},
			{Text: "Why did you move?", Next: merchantEnd},
			{Text: "How interesting, I wanted to ask you about something else...", Next: nil}, // set below
			{Text: "Goodbye", Next: nil},
		},
	}
	merchantRoot = &ConversationNode{
		Text: "Welcome! How can I help you?",
		Choices: []ConversationChoice{
			{Text: "Can I buy something?", Next: merchantTrade},
			{Text: "Any news?", Next: merchantAnyNews},
			{Text: "Where are you from?", Next: merchantWhereFrom},
			{Text: "Goodbye", Next: nil},
		},
	}
	// Set "How interesting..." choices to point to root
	merchantEnd.Choices[1].Next = merchantRoot
	merchantTrade.Choices[2].Next = merchantRoot
	merchantTrade.Choices[3].Next = merchantRoot
	merchantAnyNews.Choices[2].Next = merchantRoot
	merchantWhereFrom.Choices[2].Next = merchantRoot
	if merchantTrade.Choices[1].Next != nil {
		merchantTrade.Choices[1].Next.Choices[2].Next = merchantRoot
	}
	g.conversations["Merchant"] = merchantRoot

	// Alchemist conversation
	var alchemistRoot *ConversationNode
	alchemistEnd := &ConversationNode{
		Text: "Farewell, may your path be clear.",
		Choices: []ConversationChoice{
			{Text: "Goodbye", Next: nil},
			{Text: "How interesting, I wanted to ask you about something else...", Next: nil}, // set below
		},
	}
	alchemistTeach := &ConversationNode{
		Text: "Alchemy is a lifelong pursuit. Start with herbs.",
		Choices: []ConversationChoice{
			{Text: "Which herbs?", Next: alchemistEnd},
			{Text: "Is it dangerous?", Next: alchemistEnd},
			{Text: "How interesting, I wanted to ask you about something else...", Next: nil}, // set below
			{Text: "Goodbye", Next: nil},
		},
	}
	alchemistWork := &ConversationNode{
		Text: "A potion for better memory.",
		Choices: []ConversationChoice{
			{Text: "Can I try it?", Next: alchemistEnd},
			{Text: "Does it work?", Next: alchemistEnd},
			{Text: "How interesting, I wanted to ask you about something else...", Next: nil}, // set below
			{Text: "Goodbye", Next: nil},
		},
	}
	alchemistMagic := &ConversationNode{
		Text: "Of course. Magic is everywhere.",
		Choices: []ConversationChoice{
			{Text: "Show me!", Next: alchemistEnd},
			{Text: "I don't believe you.", Next: alchemistEnd},
			{Text: "How interesting, I wanted to ask you about something else...", Next: nil}, // set below
			{Text: "Goodbye", Next: nil},
		},
	}
	alchemistRoot = &ConversationNode{
		Text: "Greetings, traveler. What knowledge do you seek?",
		Choices: []ConversationChoice{
			{Text: "Can you teach me alchemy?", Next: alchemistTeach},
			{Text: "What are you working on?", Next: alchemistWork},
			{Text: "Do you believe in magic?", Next: alchemistMagic},
			{Text: "Goodbye", Next: nil},
		},
	}
	// Set "How interesting..." choices to point to root
	alchemistEnd.Choices[1].Next = alchemistRoot
	alchemistTeach.Choices[2].Next = alchemistRoot
	alchemistWork.Choices[2].Next = alchemistRoot
	alchemistMagic.Choices[2].Next = alchemistRoot
	g.conversations["Alchemist"] = alchemistRoot
}

func (g *Game) addToInventory(item string, count int) {
	maxPerCell := 5
	if item == "Wood" {
		maxPerCell = 5
	}
	// Try to stack first, up to maxPerCell
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			slot := &g.inventory[y][x]
			if slot.Item == item && slot.Count > 0 && slot.Count < maxPerCell {
				add := count
				if slot.Count+add > maxPerCell {
					add = maxPerCell - slot.Count
				}
				slot.Count += add
				count -= add
				if count <= 0 {
					return
				}
			}
		}
	}
	// Find first empty slot(s)
	for y := 0; y < 8 && count > 0; y++ {
		for x := 0; x < 8 && count > 0; x++ {
			slot := &g.inventory[y][x]
			if slot.Item == "" || slot.Count == 0 {
				add := count
				if add > maxPerCell {
					add = maxPerCell
				}
				slot.Item = item
				slot.Count = add
				count -= add
			}
		}
	}
}

func (g *Game) hasItem(item string, count int) bool {
	total := 0
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			slot := &g.inventory[y][x]
			if slot.Item == item {
				total += slot.Count
				if total >= count {
					return true
				}
			}
		}
	}
	return false
}

func (g *Game) removeItem(item string, count int) {
	for y := 0; y < 8 && count > 0; y++ {
		for x := 0; x < 8 && count > 0; x++ {
			slot := &g.inventory[y][x]
			if slot.Item == item && slot.Count > 0 {
				if slot.Count > count {
					slot.Count -= count
					return
				} else {
					count -= slot.Count
					slot.Count = 0
					slot.Item = ""
				}
			}
		}
	}
}
