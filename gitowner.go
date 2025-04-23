package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
	"strings" // Necesario para manejo de strings
	"time"

	"github.com/BurntSushi/toml" // Importar la librería TOML
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	// "golang.org/x/exp/maps" // Ya no es estrictamente necesario si no usamos maps.Keys
)

// OwnerScore represents a user and their score
type OwnerScore struct {
	Email       string
	Score       float64
	RepoCount   int
	RawScore    float64
	AliasesUsed []string // Opcional: Para mostrar qué alias se fusionaron
}

// --- Estructura para el archivo TOML de Aliases ---
type AliasConfig struct {
	Aliases map[string][]string `toml:"aliases"` // canonical_email -> [alias1, alias2, ...]
}

// --- Función para cargar y procesar aliases ---
func loadAliases(filePath string) (map[string]string, error) {
	aliasMap := make(map[string]string) // Mapa final: alias_email -> canonical_email
	if filePath == "" {
		return aliasMap, nil // No se proporcionó archivo, devolver mapa vacío
	}

	fmt.Printf("Attempting to load aliases from: %s\n", filePath)
	data, err := os.ReadFile(filePath)
	if err != nil {
		// Si el archivo no existe, no es necesariamente un error fatal si el flag fue opcional
		if os.IsNotExist(err) {
			fmt.Printf("Warning: Alias file not found at %s, proceeding without aliases.\n", filePath)
			return aliasMap, nil // Devuelve mapa vacío, no es un error de ejecución
		}
		return nil, fmt.Errorf("failed to read alias file %s: %w", filePath, err)
	}

	var config AliasConfig
	if _, err := toml.Decode(string(data), &config); err != nil {
		return nil, fmt.Errorf("failed to parse alias file %s: %w", filePath, err)
	}

	// Invertir el mapa para búsqueda rápida: alias -> canonical
	duplicates := make(map[string]string) // Para detectar si un alias apunta a múltiples canonicales
	for canonical, aliasList := range config.Aliases {
		canonical = strings.ToLower(strings.TrimSpace(canonical)) // Normalizar canonical
		if canonical == "" {
			continue
		} // Ignorar entradas vacías

		// Asegurar que el canonical no sea ya un alias de otro
		if existingCanonical, isAlias := aliasMap[canonical]; isAlias {
			fmt.Fprintf(os.Stderr, "Warning: Canonical email '%s' is already listed as an alias for '%s'. Check your aliases file.\n", canonical, existingCanonical)
			// Decide cómo manejar esto, aquí simplemente lo ignoramos como canonical si ya es alias.
			continue
		}

		for _, alias := range aliasList {
			alias = strings.ToLower(strings.TrimSpace(alias)) // Normalizar alias
			if alias == "" || alias == canonical {
				continue
			} // Ignorar alias vacíos o iguales al canonical

			if existingCanonical, exists := aliasMap[alias]; exists {
				// Este alias ya estaba mapeado a otro canonical!
				if existingCanonical != canonical {
					fmt.Fprintf(os.Stderr, "Warning: Alias '%s' is mapped to multiple canonical emails ('%s' and '%s'). Using '%s'. Check your aliases file.\n", alias, existingCanonical, canonical, canonical)
					// Podríamos decidir mantener el primero, el último, o dar error. Aquí sobreescribimos (último gana).
				}
				duplicates[alias] = canonical // Registrar el conflicto (último gana)
			}
			if _, isAlsoCanonical := config.Aliases[alias]; isAlsoCanonical {
				fmt.Fprintf(os.Stderr, "Warning: Email '%s' is listed both as an alias (for '%s') and as a canonical email itself. Using it as an alias.\n", alias, canonical)
			}
			aliasMap[alias] = canonical
		}
	}
	// Aplicar los duplicados detectados (último gana)
	for alias, canonical := range duplicates {
		aliasMap[alias] = canonical
	}

	fmt.Printf("Loaded %d alias mappings.\n", len(aliasMap))
	return aliasMap, nil
}

// --- Función para obtener el email canónico ---
func getCanonicalEmail(email string, aliasMap map[string]string) string {
	normalizedEmail := strings.ToLower(strings.TrimSpace(email))
	if canonical, ok := aliasMap[normalizedEmail]; ok {
		return canonical // Devuelve el email canónico mapeado
	}
	return normalizedEmail // Devuelve el email original (normalizado) si no es un alias
}

// processRepoCommits analiza un único repositorio y actualiza los mapas globales.
// Devuelve un error si no puede procesar el repositorio.
func processRepoCommits(repoPath string, tau float64, aliasMap map[string]string, userScores map[string]float64, userRepos map[string]map[string]struct{}, userAliases map[string]map[string]struct{}) error { // Añadido userAliases
	fmt.Printf("Processing repository: %s\n", repoPath)
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return fmt.Errorf("failed to open repository %s: %w", repoPath, err)
	}

	ref, err := repo.Head()
	if err != nil {
		return fmt.Errorf("failed to get HEAD for repository %s: %w", repoPath, err)
	}

	commitIter, err := repo.Log(&git.LogOptions{From: ref.Hash()})
	if err != nil {
		return fmt.Errorf("failed to get commit log for repository %s: %w", repoPath, err)
	}

	now := time.Now()

	err = commitIter.ForEach(func(c *object.Commit) error {
		if c == nil || c.Author.When.IsZero() {
			return nil
		}
		rawAuthorEmail := c.Author.Email
		if rawAuthorEmail == "" {
			return nil
		}

		// Obtener el email canónico usando el mapa de alias
		canonicalEmail := getCanonicalEmail(rawAuthorEmail, aliasMap)
		originalNormalized := strings.ToLower(strings.TrimSpace(rawAuthorEmail))

		daysAgo := now.Sub(c.Author.When).Hours() / 24
		if daysAgo < 0 {
			daysAgo = 0
		}
		weight := math.Exp(-daysAgo / tau)
		userScores[canonicalEmail] += weight // Usar el email canónico como clave

		// Registrar que este usuario (canónico) contribuyó a este repo
		if _, ok := userRepos[canonicalEmail]; !ok {
			userRepos[canonicalEmail] = make(map[string]struct{})
		}
		userRepos[canonicalEmail][repoPath] = struct{}{}

		// Registrar qué alias se usó para este usuario canónico (si es diferente)
		if originalNormalized != canonicalEmail {
			if _, ok := userAliases[canonicalEmail]; !ok {
				userAliases[canonicalEmail] = make(map[string]struct{})
			}
			userAliases[canonicalEmail][originalNormalized] = struct{}{}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("error iterating commits in %s: %w", repoPath, err)
	}

	fmt.Printf("Finished processing %s.\n", repoPath)
	return nil
}

func main() {
	// --- Parámetros ---
	tau := flag.Float64("tau", 365.0, "Temporal decay parameter (in days)")
	count := flag.Int("count", 10, "Number of most likely owners to display")
	bonusPerRepo := flag.Float64("bonus-per-repo", 0.1, "Multiplicative bonus factor per additional repository (e.g., 0.1 means +10% for the 2nd repo)")
	aliasesFile := flag.String("aliases-file", "", "Optional path to a TOML file defining email aliases (e.g., aliases.toml)") // Nuevo flag
	flag.Parse()

	// --- Validación de Entrada ---
	repoPaths := flag.Args()
	if len(repoPaths) == 0 {
		fmt.Println("Usage: go run main.go [--tau=...] [--count=...] [--bonus-per-repo=...] [--aliases-file=...] <local_repo_path1> [local_repo_path2] ...")
		os.Exit(1)
	}
	if *bonusPerRepo < 0 {
		fmt.Println("Error: --bonus-per-repo cannot be negative.")
		os.Exit(1)
	}

	// --- Cargar Aliases (antes de procesar repos) ---
	aliasMap, err := loadAliases(*aliasesFile)
	if err != nil {
		// loadAliases maneja el caso 'no encontrado' sin error si el flag está vacío.
		// Si se especificó un archivo y falló la carga/parseo, sí es un error.
		if *aliasesFile != "" {
			fmt.Fprintf(os.Stderr, "Error loading aliases: %v\n", err)
			os.Exit(1)
		}
		// Si no se especificó archivo y hubo otro error (poco probable), o si sólo fue 'no encontrado', ya se imprimió warning.
	}

	// --- Procesamiento ---
	globalUserScores := make(map[string]float64)            // canonical_email -> Puntuación base acumulada
	userRepos := make(map[string]map[string]struct{})       // canonical_email -> Set de paths de repos
	userAliasesUsed := make(map[string]map[string]struct{}) // canonical_email -> Set de alias emails usados para este canonical

	fmt.Printf("Analyzing %d repositories with tau=%.1f days...\n", len(repoPaths), *tau)

	for _, repoPath := range repoPaths {
		// Pasar aliasMap a la función de procesamiento
		err := processRepoCommits(repoPath, *tau, aliasMap, globalUserScores, userRepos, userAliasesUsed)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Skipping repository %s due to error: %v\n", repoPath, err)
		}
	}

	// --- Cálculo Final y Ordenación ---
	if len(globalUserScores) == 0 {
		fmt.Println("No commit data found or processed successfully.")
		os.Exit(0)
	}

	owners := make([]OwnerScore, 0, len(globalUserScores))
	for canonicalEmail, rawScore := range globalUserScores {
		repoSet := userRepos[canonicalEmail]
		repoCount := len(repoSet)

		aliasesSet := userAliasesUsed[canonicalEmail]
		aliases := make([]string, 0, len(aliasesSet))
		for alias := range aliasesSet {
			aliases = append(aliases, alias)
		}
		sort.Strings(aliases) // Ordenar para consistencia

		bonusFactor := 1.0
		if repoCount > 1 {
			bonusFactor = 1.0 + (float64(repoCount-1) * (*bonusPerRepo))
		}
		finalScore := rawScore * bonusFactor

		owners = append(owners, OwnerScore{
			Email:       canonicalEmail, // Usar siempre el email canónico
			Score:       finalScore,
			RepoCount:   repoCount,
			RawScore:    rawScore,
			AliasesUsed: aliases, // Guardar los alias que se fusionaron en este
		})
	}

	sort.Slice(owners, func(i, j int) bool {
		if owners[i].Score == owners[j].Score {
			if owners[i].RepoCount == owners[j].RepoCount {
				return owners[i].Email < owners[j].Email // Orden alfabético como último desempate
			}
			return owners[i].RepoCount > owners[j].RepoCount
		}
		return owners[i].Score > owners[j].Score
	})

	// --- Salida ---
	fmt.Println("\n--- Top Likely Owners ---")
	fmt.Printf("Showing top %d contributors based on recent activity across %d specified repositories.\n", *count, len(repoPaths))
	fmt.Printf("Bonus per additional repo: %.1f%%\n", *bonusPerRepo*100)
	if len(aliasMap) > 0 {
		fmt.Printf("Aliases loaded from: %s\n", *aliasesFile)
	} else if *aliasesFile != "" {
		fmt.Printf("Alias file specified (%s) but no aliases loaded (e.g., file not found or empty).\n", *aliasesFile)
	} else {
		fmt.Println("No alias file specified.")
	}
	fmt.Println("")

	limit := *count
	if len(owners) < limit {
		limit = len(owners)
	}

	for i, owner := range owners[:limit] {
		aliasInfo := ""
		if len(owner.AliasesUsed) > 0 {
			aliasInfo = fmt.Sprintf(" (aliases: %s)", strings.Join(owner.AliasesUsed, ", "))
		}
		fmt.Printf("%d. %s (Score: %.2f, Repos: %d)%s\n",
			i+1,
			owner.Email,
			owner.Score,
			owner.RepoCount,
			aliasInfo) // Añadir información de alias si existe
	}
}
