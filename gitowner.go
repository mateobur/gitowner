package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
	"strings" // Needed for string manipulation
	"time"

	"github.com/BurntSushi/toml" // Import TOML library
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	// "golang.org/x/exp/maps" // No longer strictly necessary if not using maps.Keys
)

// OwnerScore represents a user and their score
type OwnerScore struct {
	Email       string
	Score       float64
	RepoCount   int
	RawScore    float64
	AliasesUsed []string // Optional: To show which aliases were merged
}

// --- Structure for the TOML Aliases File ---
type AliasConfig struct {
	Aliases map[string][]string `toml:"aliases"` // canonical_email -> [alias1, alias2, ...]
}

// --- Function to load and process aliases ---
func loadAliases(filePath string) (map[string]string, error) {
	aliasMap := make(map[string]string) // Final map: alias_email -> canonical_email
	if filePath == "" {
		return aliasMap, nil // No file provided, return empty map
	}

	fmt.Printf("Attempting to load aliases from: %s\n", filePath)
	data, err := os.ReadFile(filePath)
	if err != nil {
		// If the file doesn't exist, it's not necessarily a fatal error if the flag was optional
		if os.IsNotExist(err) {
			fmt.Printf("Warning: Alias file not found at %s, proceeding without aliases.\n", filePath)
			return aliasMap, nil // Return empty map, not an execution error
		}
		return nil, fmt.Errorf("failed to read alias file %s: %w", filePath, err)
	}

	var config AliasConfig
	if _, err := toml.Decode(string(data), &config); err != nil {
		return nil, fmt.Errorf("failed to parse alias file %s: %w", filePath, err)
	}

	// Invert the map for quick lookup: alias -> canonical
	duplicates := make(map[string]string) // To detect if an alias points to multiple canonicals
	for canonical, aliasList := range config.Aliases {
		canonical = strings.ToLower(strings.TrimSpace(canonical)) // Normalize canonical
		if canonical == "" {
			continue
		} // Ignore empty entries

		// Ensure the canonical is not already an alias for another
		if existingCanonical, isAlias := aliasMap[canonical]; isAlias {
			fmt.Fprintf(os.Stderr, "Warning: Canonical email '%s' is already listed as an alias for '%s'. Check your aliases file.\n", canonical, existingCanonical)
			// Decide how to handle this, here we just ignore it as canonical if it's already an alias.
			continue
		}

		for _, alias := range aliasList {
			alias = strings.ToLower(strings.TrimSpace(alias)) // Normalize alias
			if alias == "" || alias == canonical {
				continue
			} // Ignore empty aliases or those identical to the canonical

			if existingCanonical, exists := aliasMap[alias]; exists {
				// This alias was already mapped to another canonical!
				if existingCanonical != canonical {
					fmt.Fprintf(os.Stderr, "Warning: Alias '%s' is mapped to multiple canonical emails ('%s' and '%s'). Using '%s'. Check your aliases file.\n", alias, existingCanonical, canonical, canonical)
					// We could decide to keep the first, the last, or error out. Here we overwrite (last one wins).
				}
				duplicates[alias] = canonical // Register the conflict (last one wins)
			}
			// Check if an email listed as an alias is also listed as a canonical email itself
			if _, isAlsoCanonical := config.Aliases[alias]; isAlsoCanonical {
				fmt.Fprintf(os.Stderr, "Warning: Email '%s' is listed both as an alias (for '%s') and as a canonical email itself. Using it as an alias.\n", alias, canonical)
			}
			aliasMap[alias] = canonical
		}
	}
	// Apply detected duplicates (last one wins)
	for alias, canonical := range duplicates {
		aliasMap[alias] = canonical
	}

	fmt.Printf("Loaded %d alias mappings.\n", len(aliasMap))
	return aliasMap, nil
}

// --- Function to get the canonical email ---
func getCanonicalEmail(email string, aliasMap map[string]string) string {
	normalizedEmail := strings.ToLower(strings.TrimSpace(email))
	if canonical, ok := aliasMap[normalizedEmail]; ok {
		return canonical // Returns the mapped canonical email
	}
	return normalizedEmail // Returns the original (normalized) email if it's not an alias
}

// processRepoCommits analyzes a single repository and updates the global maps.
// Returns an error if it cannot process the repository.
func processRepoCommits(repoPath string, tau float64, aliasMap map[string]string, userScores map[string]float64, userRepos map[string]map[string]struct{}, userAliases map[string]map[string]struct{}) error { // Added userAliases
	fmt.Printf("Processing repository: %s\n", repoPath)
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return fmt.Errorf("failed to open repository %s: %w", repoPath, err)
	}

	ref, err := repo.Head()
	if err != nil {
		// Could be an empty repo or one without commits
		return fmt.Errorf("failed to get HEAD for repository %s: %w", repoPath, err)
	}

	commitIter, err := repo.Log(&git.LogOptions{From: ref.Hash()})
	if err != nil {
		return fmt.Errorf("failed to get commit log for repository %s: %w", repoPath, err)
	}

	now := time.Now()

	err = commitIter.ForEach(func(c *object.Commit) error {
		// Ignore nil commits or those with zero time (can happen with merges/errors)
		if c == nil || c.Author.When.IsZero() {
			return nil
		}
		rawAuthorEmail := c.Author.Email
		// Ignore commits with empty author emails
		if rawAuthorEmail == "" {
			return nil
		}

		// Get the canonical email using the alias map
		canonicalEmail := getCanonicalEmail(rawAuthorEmail, aliasMap)
		originalNormalized := strings.ToLower(strings.TrimSpace(rawAuthorEmail))

		daysAgo := now.Sub(c.Author.When).Hours() / 24
		// Ensure daysAgo is not negative (in case of clock skew)
		if daysAgo < 0 {
			daysAgo = 0
		}
		weight := math.Exp(-daysAgo / tau)
		userScores[canonicalEmail] += weight // Use the canonical email as the key

		// Record that this (canonical) user contributed to this repo
		if _, ok := userRepos[canonicalEmail]; !ok {
			userRepos[canonicalEmail] = make(map[string]struct{})
		}
		userRepos[canonicalEmail][repoPath] = struct{}{}

		// Record which alias was used for this canonical user (if it was different from the canonical)
		if originalNormalized != canonicalEmail {
			if _, ok := userAliases[canonicalEmail]; !ok {
				userAliases[canonicalEmail] = make(map[string]struct{})
			}
			userAliases[canonicalEmail][originalNormalized] = struct{}{}
		}

		return nil
	})
	if err != nil {
		// Report error iterating commits, but allow main function to continue if desired
		return fmt.Errorf("error iterating commits in %s: %w", repoPath, err)
	}

	fmt.Printf("Finished processing %s.\n", repoPath)
	return nil // Success for this repository
}

func main() {
	// --- Parameters ---
	tau := flag.Float64("tau", 365.0, "Temporal decay parameter (in days)")
	count := flag.Int("count", 10, "Number of most likely owners to display")
	bonusPerRepo := flag.Float64("bonus-per-repo", 0.1, "Multiplicative bonus factor per additional repository (e.g., 0.1 means +10% for the 2nd repo)")
	aliasesFile := flag.String("aliases-file", "", "Optional path to a TOML file defining email aliases (e.g., aliases.toml)") // New flag
	flag.Parse()

	// --- Input Validation ---
	repoPaths := flag.Args()
	if len(repoPaths) == 0 {
		fmt.Println("Usage: go run main.go [--tau=...] [--count=...] [--bonus-per-repo=...] [--aliases-file=...] <local_repo_path1> [local_repo_path2] ...")
		os.Exit(1)
	}
	if *bonusPerRepo < 0 {
		fmt.Println("Error: --bonus-per-repo cannot be negative.")
		os.Exit(1)
	}

	// --- Load Aliases (before processing repos) ---
	aliasMap, err := loadAliases(*aliasesFile)
	if err != nil {
		// loadAliases handles the 'not found' case gracefully if the flag was empty.
		// Only exit if a file was specified and it failed to load/parse.
		if *aliasesFile != "" {
			fmt.Fprintf(os.Stderr, "Error loading aliases: %v\n", err)
			os.Exit(1)
		}
		// If no file was specified or only a 'not found' warning occurred, continue.
	}

	// --- Processing ---
	// Global maps to accumulate data across all repositories
	globalUserScores := make(map[string]float64)            // canonical_email -> Accumulated base score
	userRepos := make(map[string]map[string]struct{})       // canonical_email -> Set of repo paths contributed to
	userAliasesUsed := make(map[string]map[string]struct{}) // canonical_email -> Set of alias emails used for this canonical

	fmt.Printf("Analyzing %d repositories with tau=%.1f days...\n", len(repoPaths), *tau)

	// Iterate over each provided repository path
	for _, repoPath := range repoPaths {
		// Pass aliasMap and accumulating maps to the processing function
		err := processRepoCommits(repoPath, *tau, aliasMap, globalUserScores, userRepos, userAliasesUsed)
		if err != nil {
			// Print a warning if a repo fails, but continue with the others
			fmt.Fprintf(os.Stderr, "Warning: Skipping repository %s due to error: %v\n", repoPath, err)
		}
	}

	// --- Final Calculation and Sorting ---
	if len(globalUserScores) == 0 {
		fmt.Println("No commit data found or processed successfully.")
		os.Exit(0)
	}

	// Convert accumulated data into OwnerScore slice, applying the bonus
	owners := make([]OwnerScore, 0, len(globalUserScores))
	for canonicalEmail, rawScore := range globalUserScores {
		repoSet := userRepos[canonicalEmail] // The set of repos for this user
		repoCount := len(repoSet)

		aliasesSet := userAliasesUsed[canonicalEmail] // The set of aliases used for this canonical email
		aliases := make([]string, 0, len(aliasesSet))
		for alias := range aliasesSet {
			aliases = append(aliases, alias)
		}
		sort.Strings(aliases) // Sort for consistent output

		// Calculate the bonus factor
		// If contributed to 1 repo, repoCount = 1, bonus = 1.0 + (1-1)*rate = 1.0
		// If contributed to 2 repos, repoCount = 2, bonus = 1.0 + (2-1)*rate = 1.0 + rate
		// If contributed to 3 repos, repoCount = 3, bonus = 1.0 + (3-1)*rate = 1.0 + 2*rate
		bonusFactor := 1.0
		if repoCount > 1 {
			bonusFactor = 1.0 + (float64(repoCount-1) * (*bonusPerRepo))
		}

		finalScore := rawScore * bonusFactor

		owners = append(owners, OwnerScore{
			Email:       canonicalEmail, // Always use the canonical email
			Score:       finalScore,
			RepoCount:   repoCount,
			RawScore:    rawScore, // Store the raw score for potential debugging/info
			AliasesUsed: aliases,  // Save the aliases that were merged into this one
		})
	}

	// Sort by final score (Score) descending
	sort.Slice(owners, func(i, j int) bool {
		// If scores are equal, break ties by repo count (more is better)
		if owners[i].Score == owners[j].Score {
			// If repo counts are also equal, break ties alphabetically by email for stable order
			if owners[i].RepoCount == owners[j].RepoCount {
				return owners[i].Email < owners[j].Email
			}
			return owners[i].RepoCount > owners[j].RepoCount
		}
		return owners[i].Score > owners[j].Score
	})

	// --- Output ---
	fmt.Println("\n--- Top Likely Owners ---")
	fmt.Printf("Showing top %d contributors based on recent activity across %d specified repositories.\n", *count, len(repoPaths))
	fmt.Printf("Bonus per additional repo: %.1f%%\n", *bonusPerRepo*100)
	if len(aliasMap) > 0 {
		fmt.Printf("Aliases loaded from: %s\n", *aliasesFile)
	} else if *aliasesFile != "" {
		// File was specified but no aliases loaded (e.g., not found, empty, or unparseable)
		fmt.Printf("Alias file specified (%s) but no aliases loaded.\n", *aliasesFile)
	} else {
		// No alias file was specified via the flag
		fmt.Println("No alias file specified.")
	}
	fmt.Println("")

	// Display only the top "count" results
	limit := *count
	if len(owners) < limit {
		limit = len(owners)
	}

	for i, owner := range owners[:limit] {
		aliasInfo := ""
		if len(owner.AliasesUsed) > 0 {
			// Add alias information if it exists for this owner
			aliasInfo = fmt.Sprintf(" (aliases: %s)", strings.Join(owner.AliasesUsed, ", "))
		}
		fmt.Printf("%d. %s (Score: %.2f, Repos: %d)%s\n",
			i+1,
			owner.Email,
			owner.Score,
			owner.RepoCount,
			aliasInfo)
	}
}
