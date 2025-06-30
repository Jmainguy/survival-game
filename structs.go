package main

import (
	"image"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/audio"
	"github.com/lafriks/go-tiled"
)

type NPC struct {
	pos      image.Point
	dir      int
	name     string
	sprite   *ebiten.Image
	anim     int
	animTick int
	moveTick int         // for random movement timing
	target   image.Point // target position for smooth movement
	moving   bool
}

type ConversationNode struct {
	Text    string
	Choices []ConversationChoice
}

type ConversationChoice struct {
	Text string
	Next *ConversationNode
	// Optionally, add effects or triggers here
}

type InventorySlot struct {
	Item  string
	Count int
}

type Game struct {
	mapData            *tiled.Map
	playerPos          image.Point
	tilesetImgs        map[string]*ebiten.Image // cache for tileset images
	treesLayer         *tiled.Layer             // reference to "Trees" layer
	idleSprite         *ebiten.Image            // idle sprite sheet
	walkSprite         *ebiten.Image            // walk sprite sheet
	playerDir          int                      // direction (0=down,1=right,2=left,3=up)
	playerAnim         int                      // animation frame (0-3)
	playerAnimTick     int                      // animation tick
	moving             bool                     // is player moving
	musicPlayer        *audio.Player            // background music player
	npcs               []*NPC                   // list of NPCs
	gameOver           bool
	musicFiles         []string
	musicPlayed        []string
	audioContext       *audio.Context
	chatting           bool
	chatNPC            *NPC
	chatChoice         int                          // 0 or 1
	lastChatEnd        time.Time                    // add this field to track last chat end time
	conversations      map[string]*ConversationNode // map NPC name to root conversation node
	convNode           *ConversationNode            // current node in conversation
	lastChoiceTime     time.Time                    // add this field for chat input delay
	pendingTreeLayer   *tiled.Layer
	pendingTreeTileIdx int
	inventory          [8][8]InventorySlot
	inventoryOpen      bool
	lastInventoryTime  time.Time // add this field for inventory open/close delay

	// New fields for status bars and time
	health      float64 // 0.0 - 1.0
	social      float64 // 0.0 - 1.0
	hunger      float64 // 0.0 - 1.0
	gameMinutes int     // 0 - 1439 (24*60)
	lastTick    time.Time
	lastDrain   int // last in-game minute when drain was applied
}
