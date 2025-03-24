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

// OwnerScore representa un usuario y su puntuación
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

	cIter, err := repo.Log(&git.LogOptions{From: ref.Hash()})
	if err != nil {
		return nil, err
	}

	userScores := make(map[string]float64)
	now := time.Now()

	err = cIter.ForEach(func(c *object.Commit) error {
		author := c.Author.Email
		daysAgo := now.Sub(c.Author.When).Hours() / 24
		weight := math.Exp(-daysAgo / tau)
		userScores[author] += weight
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Convertir el mapa a un slice para poder ordenarlo
	owners := make([]OwnerScore, 0, len(userScores))
	for email, score := range userScores {
		owners = append(owners, OwnerScore{Email: email, Score: score})
	}

	// Ordenar por puntuación de mayor a menor
	sort.Slice(owners, func(i, j int) bool {
		return owners[i].Score > owners[j].Score
	})

	// Devolver solo los primeros "count" resultados o menos si no hay suficientes
	if len(owners) > count {
		owners = owners[:count]
	}

	return owners, nil
}

func main() {
	tau := flag.Float64("tau", 365.0, "Parámetro de penalización temporal (en días)")
	count := flag.Int("count", 3, "Número de propietarios más probables a mostrar")
	flag.Parse()

	if len(flag.Args()) < 1 {
		fmt.Println("Uso: go run main.go [--tau=...] [--count=...] <path_local_repo>")
		os.Exit(1)
	}

	repoPath := flag.Args()[0]
	owners, err := topOwnersLocal(repoPath, *tau, *count)
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}

	fmt.Println("Top propietarios más probables:")
	for i, owner := range owners {
		fmt.Printf("%d. %s (puntuación: %.2f)\n", i+1, owner.Email, owner.Score)
	}
}
