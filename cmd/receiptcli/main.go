package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/joho/godotenv"
)

var TaxReliefsWithProof = map[string]int{
	"medical_expenses_individual":     10000,  // Receipt from clinics, hospitals, or pharmacies (up to RM8,000 general + RM2,000 for full medical checkup)
	"medical_expenses_parents":        8000,   // Receipts with parent's name and medical diagnosis from registered medical practitioner
	"education_fees_self":             7000,   // Official fee receipts from approved local institutions for specific fields
	"lifestyle":                       2500,   // Receipts for books, electronics, gym, internet, etc.
	"lifestyle_additional":            500,    // Receipts for sports equipment, e-newspapers, and entrance fees to sports facilities
	"breastfeeding_equipment":         1000,   // Receipts showing breastfeeding product and purchase date (child ≤ 2 years old)
	"childcare_kinder":                3000,   // Receipts from registered childcare/kindergarten centers (child ≤ 6 years old)
	"sspn_net_savings":                8000,   // Deposit statements from SSPN-i/SSPN-i Plus issued by PTPTN
	"life_insurance_epf":              7000,   // Annual premium statements or payment slips (RM3,000 life + RM4,000 EPF)
	"education_or_medical_insurance":  3000,   // Insurance premium receipts for education or medical coverage
	"prs_private_retirement_scheme":   3000,   // Contribution statements from PRS providers
	"zakat_fitrah":                    999999, // Official zakat/fitrah payment receipts (no limit, but capped by amount of tax payable)
	"donations_approved_institutions": 999999, // Official receipts from LHDN-approved charitable bodies (subject to max 10% of aggregate income)
}

func CleanReceiptText(input string) string {
	// Remove non-ASCII characters, weird symbols
	reg, _ := regexp.Compile(`[^\x00-\x7F]+`)
	input = reg.ReplaceAllString(input, "")

	// Collapse multiple spaces
	input = regexp.MustCompile(`\s+`).ReplaceAllString(input, " ")

	// Trim whitespace
	return strings.TrimSpace(input)
}

// 🧠 Structured prompt for Fireworks
func CreateExtractionPrompt(receipt string) string {
	return fmt.Sprintf(`
You are an intelligent assistant that extracts structured data from receipts.

Extract the following information from this receipt:
- Store or Merchant Name
- Date of the receipt
- Total amount spent
- List of purchased items (name, quantity if available, price)
- Category of expense (e.g. groceries, medical, education, fuel, utilities, dining, electronics, clothing, other)
- Whether this receipt is potentially tax-deductible (true/false)
- Reason for tax-deductibility, if applicable

Output the result in this JSON format:

{
  "merchant": "",
  "date": "",
  "total": "",
  "items": [
    { "name": "", "quantity": 1, "price": "" }
  ],
  "category": "",
  "tax_deductible": false,
  "deduction_reason": ""
}

Receipt text:

%s
`, receipt)
}

func GetContextFromText(prompt string) (string, error) {
	apiKey := os.Getenv("FIREWORKS_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("missing FIREWORKS_API_KEY environment variable")
	}

	apiUrl := "https://api.fireworks.ai/inference/v1/chat/completions"
	var jsonData = []byte(fmt.Sprintf(`{
  "model": "accounts/ezekielchow94-c13c30/deployedModels/llama-v3p2-3b-instruct-ujw7grz3",
  "max_tokens": 16384,
  "top_p": 1,
  "top_k": 40,
  "presence_penalty": 0,
  "frequency_penalty": 0,
  "temperature": 0.6,
  "messages": [
    {
      "role": "user",
      "content": "%s"
    }
  ]
}`, prompt))

	req, err := http.NewRequest(http.MethodPost, apiUrl, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Println(err)
		return "", err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	bodyString := string(bodyBytes)
	fmt.Println(bodyString)

	fmt.Println("response Status:", resp)

	return "", nil
}

func GetKeywordsFromFile(fileInfo fs.DirEntry, uid string, gid string, absPath string, inputFile string, outputFile string) error {

	cmd := exec.Command(
		"docker", "run", "--rm",
		"-u", uid+":"+gid,
		"-v", absPath+":/data",
		"tesseractshadow/tesseract4re",
		"tesseract",
		inputFile, outputFile, "-l", "eng",
	)

	log.Printf("Getting keywords from %s", fileInfo.Name())
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error processing %s: %v\nOutput: %s", fileInfo.Name(), err, string(out))
	}

	return nil
}

func ConvertPDFToImage(fileInfo fs.DirEntry, uid string, gid string, absPath string, inputFile string, outputFile string) error {
	convertCmd := exec.Command(
		"docker", "run", "--rm",
		"-u", uid+":"+gid,
		"-v", absPath+":/data",
		"receipt-poppler", "-png", inputFile, outputFile,
	)

	convertCmd.Stdout = os.Stdout
	convertCmd.Stderr = os.Stderr

	log.Printf("Converting %s to PNG...", fileInfo.Name())
	if err := convertCmd.Run(); err != nil {
		return fmt.Errorf("error converting PDF: %w", err)
	}

	return nil
}

func MergeSplitTextFiles(absDir string, baseFilename string) (string, error) {
	// Create an absolute pattern for globbing
	pattern := filepath.Join(absDir, baseFilename+"-*.txt")

	files, err := filepath.Glob(pattern)
	if err != nil {
		return "", fmt.Errorf("failed to glob files: %w", err)
	}
	if len(files) == 0 {
		return "", fmt.Errorf("no split files found for pattern: %s", pattern)
	}

	// Sort the files to maintain order (e.g., -1.txt, -2.txt, ...)
	sort.Strings(files)

	// Create the final merged output file
	mergedPath := filepath.Join(absDir, baseFilename+".txt")
	mergedFile, err := os.Create(mergedPath)
	if err != nil {
		return "", fmt.Errorf("failed to create merged file: %w", err)
	}
	defer mergedFile.Close()

	for _, filePath := range files {
		srcFile, err := os.Open(filePath)
		if err != nil {
			return "", fmt.Errorf("failed to open %s: %w", filePath, err)
		}

		_, err = io.Copy(mergedFile, srcFile)
		srcFile.Close()
		if err != nil {
			return "", fmt.Errorf("failed to write content from %s: %w", filePath, err)
		}

		// Delete the split file after merging
		if err := os.Remove(filePath); err != nil {
			return "", fmt.Errorf("failed to delete %s: %w", filePath, err)
		}
	}

	fmt.Printf("Merged files into: %s\n", mergedPath)
	return mergedPath, nil
}

func CheckEnvVars(keys []string) (missing []string) {
	for _, key := range keys {
		if val, exists := os.LookupEnv(key); !exists || val == "" {
			missing = append(missing, key)
		}
	}
	return
}

func run() error {

	projectRoot := filepath.Join("..", "..")
	envPath := filepath.Join(projectRoot, ".env")
	err := godotenv.Load(envPath)
	if err != nil {
		log.Fatalf("Error loading .env file from %s: %v", envPath, err)
	}

	requiredEnvs := []string{"FIREWORKS_API_KEY"}
	missing := CheckEnvVars(requiredEnvs)

	if len(missing) > 0 {
		log.Fatalf("Missing required environment variables: %v\n", missing)
	}

	receiptDir := "../../data"
	absPath, err := filepath.Abs(receiptDir)
	if err != nil {
		log.Fatalf("Failed to get absolute path: %v", err)
	}

	currentUser, err := user.Current()
	if err != nil {
		return fmt.Errorf("cannot get current user: %w", err)
	}

	uid := currentUser.Uid
	gid := currentUser.Gid

	// Files require merging
	filesNeedMerging := []string{}

	// Convert all pdfs to png first
	err = filepath.WalkDir(absPath, func(path string, fileInfo fs.DirEntry, err error) error {
		if err != nil || fileInfo.IsDir() || fileInfo.Name() == ".gitkeep" {
			return nil
		}

		if strings.HasSuffix(strings.ToLower(fileInfo.Name()), ".pdf") {
			filesNeedMerging = append(filesNeedMerging, strings.TrimSuffix(fileInfo.Name(), filepath.Ext(fileInfo.Name())))

			inputFile := "/data/input/" + fileInfo.Name()
			outputBase := strings.TrimSuffix(fileInfo.Name(), filepath.Ext(fileInfo.Name()))
			outputPrefix := "/data/input/" + outputBase

			err = ConvertPDFToImage(fileInfo, uid, gid, absPath, inputFile, outputPrefix)
			if err != nil {
				return err
			}

			return nil
		}

		return nil
	})

	if err != nil {
		log.Fatalf("Error walking directory to convert pdfs: %v", err)
	}

	err = filepath.WalkDir(absPath, func(path string, fileInfo fs.DirEntry, err error) error {
		if err != nil || fileInfo.IsDir() || fileInfo.Name() == ".gitkeep" {
			return nil
		}

		if strings.HasSuffix(strings.ToLower(fileInfo.Name()), ".png") || strings.HasSuffix(fileInfo.Name(), ".jpeg") || strings.HasSuffix(fileInfo.Name(), ".jpg") {

			inputFile := "/data/input/" + fileInfo.Name()
			outputFile := "/data/output/" + strings.TrimSuffix(fileInfo.Name(), filepath.Ext(fileInfo.Name()))

			err = GetKeywordsFromFile(fileInfo, uid, gid, absPath, inputFile, outputFile)
			if err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		log.Fatalf("Error generating keywords: %v", err)
	}

	// Merge keywords files
	absPath, err = filepath.Abs("../../data/output")
	if err != nil {
		log.Fatalf("Failed to get absolute path: %v", err)
		return err
	}

	filesWithExtractedTxt := []string{}
	for _, filename := range filesNeedMerging {
		filePath, err := MergeSplitTextFiles(absPath, filename)
		if err != nil {
			log.Fatalf("Error merging files: %v", err)
			return err
		}

		filesWithExtractedTxt = append(filesWithExtractedTxt, filePath)
	}

	for _, path := range filesWithExtractedTxt {
		b, err := os.ReadFile(path)
		if err != nil {
			log.Fatalf("Error reading file: %v", err)
			return errors.New("Error reading file")
		}

		content := string(b)
		receiptText := CleanReceiptText(content)

		prompt := CreateExtractionPrompt(receiptText)
		result, err := GetContextFromText(prompt)
		if err != nil {
			log.Fatalf("Unable to get context from file: %v", err)
		}

		fmt.Println("Model Response:")
		fmt.Println(result)
	}

	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
