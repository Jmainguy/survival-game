package main

import "math/rand"

// Helper to get next random music file, ensuring all are played before repeats
func nextMusicFile(files *[]string, played *[]string) string {
	if len(*files) == 0 {
		return ""
	}
	if len(*played) == len(*files) {
		*played = []string{}
	}
	remaining := []string{}
	playedSet := map[string]struct{}{}
	for _, p := range *played {
		playedSet[p] = struct{}{}
	}
	for _, f := range *files {
		if _, ok := playedSet[f]; !ok {
			remaining = append(remaining, f)
		}
	}
	if len(remaining) == 0 {
		remaining = *files
		*played = []string{}
	}
	next := remaining[rand.Intn(len(remaining))]
	*played = append(*played, next)
	return next
}
