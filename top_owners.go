package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// OwnerScore represents a user and their score
type OwnerScore struct {
	Email string
	Score float64
}

func topOwnersLocal(repoPath string, tau float64, count int) ([]OwnerScore, error) {
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, err
	}

	ref, err := repo.Head()
	if err != nil {
		return nil, err
	}

	commitIter, err := repo.Log(&git.LogOptions{From: ref.Hash()})
	if err != nil {
		return nil, err
	}

	userScores := make(map[string]float64)
	now := time.Now()

	err = commitIter.ForEach(func(c *object.Commit) error {
		author := c.Author.Email
		daysAgo := now.Sub(c.Author.When).Hours() / 24
		weight := math.Exp(-daysAgo / tau)
		userScores[author] += weight
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Convert the map to a slice to allow sorting
	owners := make([]OwnerScore, 0, len(userScores))
	for email, score := range userScores {
		owners = append(owners, OwnerScore{Email: email, Score: score})
	}

	// Sort by score descending
	sort.Slice(owners, func(i, j int) bool {
		return owners[i].Score > owners[j].Score
	})

	// Return only the top "count" results or fewer if not enough
	if len(owners) > count {
		owners = owners[:count]
	}

	return owners, nil
}

func main() {
	tau := flag.Float64("tau", 365.0, "Temporal decay parameter (in days)")
	count := flag.Int("count", 3, "Number of most likely owners to display")
	flag.Parse()

	if len(flag.Args()) < 1 {
		fmt.Println("Usage: go run main.go [--tau=...] [--count=...] <local_repo_path>")
		os.Exit(1)
	}

	repoPath := flag.Args()[0]
	owners, err := topOwnersLocal(repoPath, *tau, *count)
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}

	fmt.Println("Top likely owners:")
	for i, owner := range owners {
		fmt.Printf("%d. %s (score: %.2f)\n", i+1, owner.Email, owner.Score)
	}
}
