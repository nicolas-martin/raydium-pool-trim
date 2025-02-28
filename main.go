package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	rpcEndpoint      = "https://solana-mainnet.rpcpool.com"
	defaultQuoteMint = "So11111111111111111111111111111111111111112" // SOL
	outputFile       = "trimmed_mainnet.json"
	raydiumURL       = "https://api.raydium.io/v2/sdk/liquidity/mainnet.json"
	raydiumTokensURL = "https://api.raydium.io/v2/sdk/token/raydium.mainnet.json"
)

// RaydiumPool represents a Raydium liquidity pool
type RaydiumPool struct {
	ID              string `json:"id"`
	BaseMint        string `json:"baseMint"`
	QuoteMint       string `json:"quoteMint"`
	LPMint          string `json:"lpMint"`
	ProgramID       string `json:"programId"`
	Authority       string `json:"authority"`
	OpenOrders      string `json:"openOrders"`
	TargetOrders    string `json:"targetOrders"`
	BaseVault       string `json:"baseVault"`
	QuoteVault      string `json:"quoteVault"`
	Version         int    `json:"version"`
	BaseDecimals    int    `json:"baseDecimals"`
	QuoteDecimals   int    `json:"quoteDecimals"`
	LPDecimals      int    `json:"lpDecimals"`
	MarketVersion   int    `json:"marketVersion"`
	MarketProgramID string `json:"marketProgramId"`
	MarketID        string `json:"marketId"`
}

// RaydiumResponse represents the API response structure
type RaydiumResponse struct {
	Name       string        `json:"name"`
	Official   []RaydiumPool `json:"official"`
	Unofficial []RaydiumPool `json:"unOfficial"`
}

// Config holds the program configuration
type Config struct {
	inputFile string
	tokenFile string
	mint      string // Single mint flag for specifying token address
	ticker    string // Added ticker field
}

// TokenInfo represents a token in Raydium's token list
type TokenInfo struct {
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	Mint     string `json:"mint"`
	Decimals int    `json:"decimals"`
}

// TokenListResponse represents the token list API response
type TokenListResponse struct {
	Official   []TokenInfo `json:"official"`
	Unofficial []TokenInfo `json:"unOfficial"`
}

// TokenPoolInfo combines token information with its pools
type TokenPoolInfo struct {
	Token TokenInfo     `json:"token"`
	Pools []RaydiumPool `json:"pools"`
}

// TokenPoolInfoList represents a list of token and pool information
type TokenPoolInfoList struct {
	Tokens []TokenPoolInfo `json:"tokens"`
}

// parseFlags parses command line flags and returns config
func parseFlags() Config {
	var config Config

	flag.StringVar(&config.inputFile, "file", "", "Path to existing pool JSON file (optional)")
	flag.StringVar(&config.tokenFile, "token-file", "", "Path to existing token list JSON file (optional)")
	flag.StringVar(&config.mint, "mint", "", "Token mint address (optional, requires --ticker)")
	flag.StringVar(&config.ticker, "ticker", "", "Token ticker symbol (required when using --mint)")

	flag.Parse()

	return config
}

// fileExists checks if a file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

// downloadFile downloads a file and shows progress
func downloadFile(url, tempFilePath string) error {
	// Create the file
	out, err := os.Create(tempFilePath)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer out.Close()

	// Get the data
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download file: %w", err)
	}
	defer resp.Body.Close()

	// Check if we got a successful response
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned status code %d", resp.StatusCode)
	}

	// Create a buffer for reading chunks
	buf := make([]byte, 32*1024) // 32KB chunks
	var totalBytes int64
	lastPrint := time.Now()

	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			totalBytes += int64(n)
			_, werr := out.Write(buf[:n])
			if werr != nil {
				return fmt.Errorf("error writing to file: %w", werr)
			}

			// Update progress every 500ms
			if time.Since(lastPrint) >= 500*time.Millisecond {
				fmt.Printf("\rDownloading... %.1f MB    ", float64(totalBytes)/(1024*1024))
				lastPrint = time.Now()
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("error reading from response: %w", err)
		}
	}
	fmt.Printf("\rDownloaded %.1f MB         \n", float64(totalBytes)/(1024*1024))

	return nil
}

// validateJSON checks if the downloaded file is a valid and complete JSON
func validateJSON(filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file for validation: %w", err)
	}
	defer file.Close()

	// Create a decoder for validation
	decoder := json.NewDecoder(file)

	// Try to decode and validate structure
	var response RaydiumResponse
	if err := decoder.Decode(&response); err != nil {
		return fmt.Errorf("invalid JSON structure: %w", err)
	}

	// Basic validation of the response
	if response.Name == "" {
		return fmt.Errorf("invalid JSON: missing name field")
	}
	if response.Official == nil {
		return fmt.Errorf("invalid JSON: missing official pools array")
	}
	if len(response.Official) == 0 {
		return fmt.Errorf("invalid JSON: empty pools array")
	}

	fmt.Printf("✅ JSON validation successful: found %d pools\n", len(response.Official))
	return nil
}

// processPoolsFile processes the downloaded JSON file and filters pools
func processPoolsFile(filePath string, baseMint string, ticker string) ([]RaydiumPool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	fmt.Println("\n🔍 Processing pools...")
	fmt.Printf("Looking for %s/SOL pairs with:\n", strings.ToUpper(ticker))
	fmt.Printf("  Base Token:  %s\n", baseMint)
	fmt.Printf("  Quote Token: %s\n\n", defaultQuoteMint)

	decoder := json.NewDecoder(file)

	if _, err := decoder.Token(); err != nil {
		return nil, fmt.Errorf("failed to read opening token: %w", err)
	}

	var matchingPools []RaydiumPool
	var currentSection string
	var officialCount, unofficialCount int

	// Helper function to process a pool
	processPool := func(pool RaydiumPool, isOfficial bool) {
		if pool.BaseMint == baseMint || pool.QuoteMint == baseMint {
			// Check if this is a token/SOL pair
			if (pool.BaseMint == baseMint && pool.QuoteMint == defaultQuoteMint) ||
				(pool.QuoteMint == baseMint && pool.BaseMint == defaultQuoteMint) {
				fmt.Printf("\n📊 Pool Details (%s):\n", map[bool]string{true: "Official", false: "Unofficial"}[isOfficial])
				fmt.Printf("  ID:              %s\n", pool.ID)
				fmt.Printf("  Base Token:      %s\n", pool.BaseMint)
				fmt.Printf("  Quote Token:     %s\n", pool.QuoteMint)
				fmt.Printf("  LP Token:        %s\n", pool.LPMint)
				fmt.Printf("  Program ID:      %s\n", pool.ProgramID)
				fmt.Printf("  Market ID:       %s\n", pool.MarketID)
				fmt.Printf("  Version:         %d\n", pool.Version)
				fmt.Printf("  Market Version:  %d\n", pool.MarketVersion)
				fmt.Printf("  Base Decimals:   %d\n", pool.BaseDecimals)
				fmt.Printf("  Quote Decimals:  %d\n", pool.QuoteDecimals)
				fmt.Printf("  LP Decimals:     %d\n", pool.LPDecimals)
				fmt.Printf("  ✨ %s/SOL pair found!\n", strings.ToUpper(ticker))
				matchingPools = append(matchingPools, pool)
			}
		}
	}

	// Process the JSON structure
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("failed to read field name: %w", err)
		}

		if key, ok := token.(string); ok {
			switch key {
			case "name":
				if _, err := decoder.Token(); err != nil {
					return nil, fmt.Errorf("failed to skip name value: %w", err)
				}
			case "official", "unOfficial":
				currentSection = key

				t, err := decoder.Token()
				if err != nil {
					return nil, fmt.Errorf("failed to read array start: %w", err)
				}
				if delim, ok := t.(json.Delim); !ok || delim != '[' {
					return nil, fmt.Errorf("expected array start, got %v", t)
				}

				for decoder.More() {
					var pool RaydiumPool
					if err := decoder.Decode(&pool); err != nil {
						return nil, fmt.Errorf("failed to decode pool: %w", err)
					}

					if currentSection == "official" {
						officialCount++
						processPool(pool, true)
					} else {
						unofficialCount++
						if unofficialCount%100000 == 0 {
							fmt.Printf("\rProcessed %dk unofficial pools...", unofficialCount/1000)
						}
						processPool(pool, false)
					}
				}

				t, err = decoder.Token()
				if err != nil {
					return nil, fmt.Errorf("failed to read array end: %w", err)
				}
				if delim, ok := t.(json.Delim); !ok || delim != ']' {
					return nil, fmt.Errorf("expected array end, got %v", t)
				}

				if currentSection == "unOfficial" {
					fmt.Printf("\rProcessed %dk unofficial pools\n", unofficialCount/1000)
				}
			}
		}
	}

	fmt.Printf("\n📈 Pool Summary:\n")
	fmt.Printf("  Total Official Pools:   %d\n", officialCount)
	fmt.Printf("  Total Unofficial Pools: %d\n", unofficialCount)
	fmt.Printf("  Found %d %s/SOL pairs\n", len(matchingPools), strings.ToUpper(ticker))
	return matchingPools, nil
}

// getTokenAddress fetches the token address from Raydium's API
func getTokenAddress(symbol string, tokenFile string) ([]*TokenInfo, error) {
	var jsonFilePath string
	var matchingTokens []*TokenInfo

	if tokenFile != "" {
		if !fileExists(tokenFile) {
			return nil, fmt.Errorf("token file does not exist: %s", tokenFile)
		}
		jsonFilePath = tokenFile
		fmt.Printf("Using provided token file: %s\n", jsonFilePath)
	} else {
		jsonFilePath = filepath.Join("tmp", fmt.Sprintf("raydium-tokens-%d.json", time.Now().UnixNano()))
		if err := downloadFile(raydiumTokensURL, jsonFilePath); err != nil {
			return nil, fmt.Errorf("failed to download token list: %w", err)
		}
	}

	file, err := os.Open(jsonFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open token file: %w", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	t, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to read opening token: %w", err)
	}
	if delim, ok := t.(json.Delim); !ok || delim != '{' {
		return nil, fmt.Errorf("expected object start, got %v", t)
	}

	symbol = strings.ToUpper(strings.TrimPrefix(symbol, "$"))
	tokenCount := 0

	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("failed to read field name: %w", err)
		}

		if keyStr, ok := key.(string); ok {
			switch keyStr {
			case "official", "unOfficial":
				t, err := decoder.Token()
				if err != nil {
					return nil, fmt.Errorf("failed to read array start: %w", err)
				}
				if delim, ok := t.(json.Delim); !ok || delim != '[' {
					return nil, fmt.Errorf("expected array start for %s, got %v", keyStr, t)
				}

				for decoder.More() {
					tokenCount++
					if tokenCount%100 == 0 {
						fmt.Printf("\rProcessed %d tokens...", tokenCount)
					}

					var token TokenInfo
					if err := decoder.Decode(&token); err != nil {
						return nil, fmt.Errorf("failed to decode token: %w", err)
					}

					if token.Symbol == symbol {
						fmt.Printf("\n✅ Found %s token (%s):\n", symbol, keyStr)
						fmt.Printf("  Name: %s\n", token.Name)
						fmt.Printf("  Mint: %s\n", token.Mint)
						fmt.Printf("  Decimals: %d\n", token.Decimals)
						matchingTokens = append(matchingTokens, &token)
					}
				}

				t, err = decoder.Token()
				if err != nil {
					return nil, fmt.Errorf("failed to read array end: %w", err)
				}
				if delim, ok := t.(json.Delim); !ok || delim != ']' {
					return nil, fmt.Errorf("expected array end for %s, got %v", keyStr, t)
				}
			default:
				_, err := decoder.Token()
				if err != nil {
					return nil, fmt.Errorf("failed to skip value: %w", err)
				}
			}
		}
	}

	t, err = decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to read closing token: %w", err)
	}
	if delim, ok := t.(json.Delim); !ok || delim != '}' {
		return nil, fmt.Errorf("expected object end, got %v", t)
	}

	fmt.Printf("\nProcessed %d tokens total\n", tokenCount)
	if len(matchingTokens) == 0 {
		return nil, fmt.Errorf("token %s not found", symbol)
	}
	return matchingTokens, nil
}

// writeFilteredPools writes or appends the filtered pools to the output file
func writeFilteredPools(tokenInfo *TokenInfo, pools []RaydiumPool) error {
	var tokenList TokenPoolInfoList

	// Try to read existing file
	if fileExists(outputFile) {
		existingFile, err := os.ReadFile(outputFile)
		if err != nil {
			return fmt.Errorf("failed to read existing output file: %w", err)
		}

		if err := json.Unmarshal(existingFile, &tokenList); err != nil {
			// If the file exists but isn't in the new format, try to read it as a single TokenPoolInfo
			var oldFormat TokenPoolInfo
			if err := json.Unmarshal(existingFile, &oldFormat); err != nil {
				return fmt.Errorf("failed to parse existing output file: %w", err)
			}
			// Convert old format to new format
			tokenList.Tokens = []TokenPoolInfo{oldFormat}
		}

		// Check if token already exists and update it
		updated := false
		for i, existing := range tokenList.Tokens {
			if existing.Token.Symbol == tokenInfo.Symbol {
				fmt.Printf("🔄 Updating existing entry for %s in the output file...\n", tokenInfo.Symbol)
				tokenList.Tokens[i] = TokenPoolInfo{
					Token: *tokenInfo,
					Pools: pools,
				}
				updated = true
				break
			}
		}

		// If token wasn't found, append it
		if !updated {
			tokenList.Tokens = append(tokenList.Tokens, TokenPoolInfo{
				Token: *tokenInfo,
				Pools: pools,
			})
		}
	} else {
		// Create new file with the token info
		tokenList.Tokens = []TokenPoolInfo{{
			Token: *tokenInfo,
			Pools: pools,
		}}
	}

	// Write back to file
	file, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(tokenList); err != nil {
		return fmt.Errorf("failed to write output file: %w", err)
	}

	fmt.Printf("✅ Successfully wrote/updated token info and %d pools to %s\n", len(pools), outputFile)
	fmt.Printf("📊 File now contains information for %d tokens\n", len(tokenList.Tokens))
	return nil
}

func main() {
	fmt.Println("🌊 Raydium Pool Fetcher")
	fmt.Println("------------------------")

	config := parseFlags()

	// Validate flags
	if config.mint != "" && config.ticker == "" {
		log.Fatalf("❌ Error: --ticker is required when using --mint\nUsage: --mint=<mint_address> --ticker=<token_symbol>")
	}

	var selectedToken *TokenInfo

	// If mint is provided, create a token info
	if config.mint != "" {
		selectedToken = &TokenInfo{
			Symbol:   config.ticker,
			Name:     fmt.Sprintf("%s (Direct Mint)", config.ticker),
			Mint:     config.mint,
			Decimals: 9, // Default to 9 decimals
		}
		fmt.Printf("Using provided mint address directly: %s\n", config.mint)
	} else {
		// Get token address from Raydium API using provided ticker
		tokens, err := getTokenAddress(config.ticker, config.tokenFile)
		if err != nil {
			log.Fatalf("❌ Failed to get %s token address: %v", config.ticker, err)
		}

		if len(tokens) > 1 {
			fmt.Printf("\n🔍 Found multiple tokens with symbol %s. Please choose one:\n", config.ticker)
			for i, token := range tokens {
				fmt.Printf("%d) %s (Mint: %s)\n", i+1, token.Name, token.Mint)
			}
			fmt.Printf("\nRe-run the command with --mint=<mint_address> --ticker=%s to use a specific token\n", config.ticker)
			os.Exit(0)
		}

		selectedToken = tokens[0]
	}

	// Update config with token address
	config.mint = selectedToken.Mint

	fmt.Printf("Base Token (%s): %s\n", config.ticker, config.mint)
	fmt.Printf("Quote Token (SOL): %s\n\n", defaultQuoteMint)

	var jsonFilePath string

	if config.inputFile != "" {
		if !fileExists(config.inputFile) {
			log.Fatalf("❌ Provided file does not exist: %s", config.inputFile)
		}
		jsonFilePath = config.inputFile
		fmt.Printf("Using provided file: %s\n", jsonFilePath)
	} else {
		if err := os.MkdirAll("tmp", 0o755); err != nil {
			log.Fatalf("❌ Failed to create tmp directory: %v", err)
		}

		jsonFilePath = filepath.Join("tmp", fmt.Sprintf("raydium-pools-%d.json", time.Now().UnixNano()))

		if err := downloadFile(raydiumURL, jsonFilePath); err != nil {
			log.Fatalf("❌ Download failed: %v", err)
		}
	}

	if err := validateJSON(jsonFilePath); err != nil {
		if config.inputFile == "" {
			os.Remove(jsonFilePath)
		}
		log.Fatalf("❌ Invalid JSON file: %v", err)
	}

	pools, err := processPoolsFile(jsonFilePath, config.mint, config.ticker)
	if err != nil {
		if config.inputFile == "" {
			os.Remove(jsonFilePath)
		}
		log.Fatalf("❌ Failed to process pools: %v", err)
	}

	if err := writeFilteredPools(selectedToken, pools); err != nil {
		log.Fatalf("❌ Failed to write filtered pools: %v", err)
	}

	if config.inputFile == "" {
		fmt.Printf("\n💡 Tip: Use --file=%s next time to skip downloading\n", jsonFilePath)
	}
	if config.tokenFile == "" && fileExists(filepath.Join("tmp", fmt.Sprintf("raydium-tokens-%d.json", time.Now().UnixNano()))) {
		fmt.Printf("💡 Tip: Use --token-file=%s next time to skip downloading token list\n", filepath.Join("tmp", fmt.Sprintf("raydium-tokens-%d.json", time.Now().UnixNano())))
	}
}
