# Git Repo Owner Analyzer

This tool analyzes the commit history of one or more local Git repositories to identify the most likely or active recent contributors (owners). It calculates a score for each author based on the frequency and recency of their commits, applies a bonus for contributing to multiple analyzed repositories, and can merge contributions from the same person using different email addresses via an alias file.

## Features

*   **Recency Weighting:** Uses an exponential decay function (`--tau` parameter) to give more weight to recent commits.
*   **Multi-Repository Analysis:** Analyzes one or multiple local Git repositories simultaneously.
*   **Cross-Repository Bonus:** Applies a configurable score bonus (`--bonus-per-repo`) for authors contributing to more than one of the analyzed repositories.
*   **Email Alias Merging:** Merges contributions from different email addresses belonging to the same person using an optional TOML alias file (`--aliases-file`).
*   **Top N Results:** Displays the top N contributors (`--count` parameter) sorted by score.

## Installation

1.  **Install Go:** Ensure you have Go installed (version 1.18 or later recommended). You can download it from [golang.org](https://golang.org/dl/).
2.  **Get Dependencies:** Open your terminal and run:
    ```bash
    go get github.com/go-git/go-git/v5
    go get github.com/BurntSushi/toml
    ```
3.  **Get the Code:** Clone this repository or download the `main.go` file.

## Usage

Run the tool from your terminal using `go run main.go`, providing the paths to the local Git repositories you want to analyze as arguments.

```bash
go run main.go [flags] /path/to/repo1 [/path/to/repo2 ...]