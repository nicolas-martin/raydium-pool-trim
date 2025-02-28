# Raydium Pool Trim

A Go utility for filtering and processing Raydium liquidity pools data on the Solana blockchain. This tool helps you extract specific token pool information from Raydium's API, focusing on particular tokens or trading pairs.

## Features

- Filter Raydium liquidity pools by specific token addresses
- Support for both official and unofficial pools
- Process pools based on token symbols or addresses
- Automatic data validation and error handling
- Output filtered pool data in JSON format

## Usage

The tool can be run with various flags to customize the filtering process:

```bash
go run main.go [flags]
```

Available flags:
- `-input`: Specify custom input file path
- `-token`: Specify custom token list file path
- `-mint`: Filter by specific token mint address
- `-ticker`: Filter by token ticker symbol

## Output

The tool generates a `trimmed_mainnet.json` file containing the filtered pool information in a structured JSON format.