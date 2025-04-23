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
	// Necesitamos esto para obtener las claves del mapa fácilmente
)

// OwnerScore represents a user and their score
type OwnerScore struct {
	Email     string
	Score     float64
	RepoCount int     // Añadido para saber en cuántos repos ha contribuido
	RawScore  float64 // Puntuación antes del bonus (opcional para debugging/info)
}

// processRepoCommits analiza un único repositorio y actualiza los mapas globales.
// Devuelve un error si no puede procesar el repositorio.
func processRepoCommits(repoPath string, tau float64, userScores map[string]float64, userRepos map[string]map[string]struct{}) error {
	fmt.Printf("Processing repository: %s\n", repoPath) // Info para el usuario
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		// En lugar de detener todo, reportamos el error y permitimos continuar con otros repos
		return fmt.Errorf("failed to open repository %s: %w", repoPath, err)
	}

	ref, err := repo.Head()
	if err != nil {
		// Podría ser un repo vacío o sin commits
		return fmt.Errorf("failed to get HEAD for repository %s: %w", repoPath, err)
	}

	commitIter, err := repo.Log(&git.LogOptions{From: ref.Hash()})
	if err != nil {
		return fmt.Errorf("failed to get commit log for repository %s: %w", repoPath, err)
	}

	now := time.Now()

	err = commitIter.ForEach(func(c *object.Commit) error {
		// Ignorar commits de merge vacíos o con errores
		if c == nil || c.Author.When.IsZero() {
			return nil
		}
		author := c.Author.Email
		// Evitar emails vacíos que a veces pueden aparecer
		if author == "" {
			return nil
		}

		daysAgo := now.Sub(c.Author.When).Hours() / 24
		// Asegurarse de que daysAgo no sea negativo (por si hay relojes desincronizados)
		if daysAgo < 0 {
			daysAgo = 0
		}
		weight := math.Exp(-daysAgo / tau)
		userScores[author] += weight

		// Registrar que este usuario contribuyó a este repo
		if _, ok := userRepos[author]; !ok {
			userRepos[author] = make(map[string]struct{})
		}
		userRepos[author][repoPath] = struct{}{} // Usamos struct{} vacía como valor eficiente para un set

		return nil
	})
	if err != nil {
		return fmt.Errorf("error iterating commits in %s: %w", repoPath, err)
	}

	fmt.Printf("Finished processing %s.\n", repoPath)
	return nil // Éxito para este repositorio
}

func main() {
	// --- Parámetros ---
	tau := flag.Float64("tau", 365.0, "Temporal decay parameter (in days)")
	count := flag.Int("count", 10, "Number of most likely owners to display") // Aumentado por defecto
	bonusPerRepo := flag.Float64("bonus-per-repo", 0.1, "Multiplicative bonus factor per additional repository contributed to (e.g., 0.1 means +10% for the 2nd repo, +20% for the 3rd, etc.)")
	flag.Parse()

	// --- Validación de Entrada ---
	repoPaths := flag.Args()
	if len(repoPaths) == 0 {
		fmt.Println("Usage: go run main.go [--tau=...] [--count=...] [--bonus-per-repo=...] <local_repo_path1> [local_repo_path2] ...")
		os.Exit(1)
	}
	if *bonusPerRepo < 0 {
		fmt.Println("Error: --bonus-per-repo cannot be negative.")
		os.Exit(1)
	}

	// --- Procesamiento ---
	// Mapas globales para acumular datos de todos los repositorios
	globalUserScores := make(map[string]float64)      // Email -> Puntuación base acumulada
	userRepos := make(map[string]map[string]struct{}) // Email -> Set de paths de repositorios a los que contribuyó

	fmt.Printf("Analyzing %d repositories with tau=%.1f days...\n", len(repoPaths), *tau)

	// Iterar sobre cada ruta de repositorio proporcionada
	for _, repoPath := range repoPaths {
		err := processRepoCommits(repoPath, *tau, globalUserScores, userRepos)
		if err != nil {
			// Imprimir un aviso si un repo falla, pero continuar con los demás
			fmt.Fprintf(os.Stderr, "Warning: Skipping repository %s due to error: %v\n", repoPath, err)
		}
	}

	// --- Cálculo Final y Ordenación ---
	if len(globalUserScores) == 0 {
		fmt.Println("No commit data found in the processed repositories.")
		os.Exit(0)
	}

	// Convertir los datos acumulados a la estructura OwnerScore, aplicando el bonus
	owners := make([]OwnerScore, 0, len(globalUserScores))
	for email, rawScore := range globalUserScores {
		repoSet := userRepos[email] // El set de repos para este usuario
		repoCount := len(repoSet)

		// Calcular el factor de bonificación
		// Si contribuyó a 1 repo, numRepos = 1, bonus = 1.0 + (1-1)*rate = 1.0
		// Si contribuyó a 2 repos, numRepos = 2, bonus = 1.0 + (2-1)*rate = 1.0 + rate
		// Si contribuyó a 3 repos, numRepos = 3, bonus = 1.0 + (3-1)*rate = 1.0 + 2*rate
		bonusFactor := 1.0
		if repoCount > 1 {
			bonusFactor = 1.0 + (float64(repoCount-1) * (*bonusPerRepo))
		}

		finalScore := rawScore * bonusFactor

		owners = append(owners, OwnerScore{
			Email:     email,
			Score:     finalScore,
			RepoCount: repoCount,
			RawScore:  rawScore, // Guardamos la puntuación raw por si es útil mostrarla
		})
	}

	// Ordenar por la puntuación final (Score) descendente
	sort.Slice(owners, func(i, j int) bool {
		// En caso de empate en score, desempatar por número de repos (más es mejor)
		if owners[i].Score == owners[j].Score {
			// Y si empatan en repos, desempatar por email para orden estable
			if owners[i].RepoCount == owners[j].RepoCount {
				return owners[i].Email < owners[j].Email
			}
			return owners[i].RepoCount > owners[j].RepoCount
		}
		return owners[i].Score > owners[j].Score
	})

	// --- Salida ---
	fmt.Println("\n--- Top Likely Owners ---")
	fmt.Printf("Showing top %d contributors based on recent activity across %d specified repositories.\n", *count, len(repoPaths))
	fmt.Printf("Bonus per additional repo: %.1f%%\n\n", *bonusPerRepo*100)

	// Mostrar solo los "count" principales resultados
	limit := *count
	if len(owners) < limit {
		limit = len(owners)
	}

	for i, owner := range owners[:limit] {
		// Podríamos añadir más información si quisiéramos, como la puntuación raw o el número de repos
		// fmt.Printf("%d. %s (Score: %.2f, Repos: %d, RawScore: %.2f)\n", i+1, owner.Email, owner.Score, owner.RepoCount, owner.RawScore)
		fmt.Printf("%d. %s (Score: %.2f, Repos: %d)\n", i+1, owner.Email, owner.Score, owner.RepoCount)
	}
}
