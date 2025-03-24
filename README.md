# top-owners

A simple CLI tool to find the top committers (potential owners) of a local Git repository, with a time-decay factor to give more recent commits higher weight.

## How It Works
- Opens the specified local Git repository.
- Logs all commits from the current HEAD.
- Calculates a weighted score for each committer’s email based on how recent their commits are, using an exponential decay function.
- Ranks the results by score and prints the top contributors.

## Installation

### With Make

1. Run `make build` to compile the binary.
2. Run `make install` to copy the binary to `$GOPATH/bin`.

### Manual

1. Clone this repository or copy the code into your own Go project.
2. Run:

   ```sh
   go build -o top-owners .
   
## Usage
```
./top-owners [--tau=365.0] [--count=3] <local_repo_path>
```
- `--tau`: Temporal decay parameter in days (default `365`).
- `--count`: Number of top owners to display (default `3`).
- `<local_repo_path>`: Path to your local Git repository.

Example:
```
./top-owners --tau=30 --count=5 /path/to/repo
```

## Notes
- The score is higher when commits are more recent.
- Authors are identified by their email.
- Adjust `tau` to change how quickly older commits lose weight.
