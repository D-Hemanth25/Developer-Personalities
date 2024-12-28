package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/generative-ai-go/genai"
	"github.com/joho/godotenv"
	"google.golang.org/api/option"
)

// GitHubUser represents the GitHub user data structure
type GitHubUser struct {
	Login             string    `json:"login"`
	Name              string    `json:"name"`
	Bio               string    `json:"bio"`
	Company           string    `json:"company"`
	Location          string    `json:"location"`
	Followers         int       `json:"followers"`
	Following         int       `json:"following"`
	PublicRepos      int       `json:"public_repos"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	ContributionsLastYear int  `json:"-"`
}

// Repository represents GitHub repository data
type Repository struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Language    string `json:"language"`
	Stars       int    `json:"stargazers_count"`
	Fork        bool   `json:"fork"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ProfileAnalysis represents the AI-generated analysis
type ProfileAnalysis struct {
	PersonalityType string
	Strengths       []string
	Areas           []string
	Suggestions     []string
	TechStack       []string
	ActivityLevel   string
}

func loadEnv() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file. Please ensure you have a .env file in the project root.")
	}
}

func getGitHubClient(token string) *http.Client {
	client := &http.Client{
		Timeout: time.Second * 10,
	}
	return client
}

func fetchGitHubData(username, token string) (*GitHubUser, []Repository) {
	client := getGitHubClient(token)
	
	// Fetch user profile
	userReq, err := http.NewRequest("GET", fmt.Sprintf("https://api.github.com/users/%s", username), nil)
	if err != nil {
		log.Fatalf("Error creating request: %v", err)
	}
	
	userReq.Header.Add("Authorization", "Bearer "+token)
	userReq.Header.Add("Accept", "application/vnd.github.v3+json")
	
	userResp, err := client.Do(userReq)
	if err != nil {
		log.Fatalf("Error fetching user data: %v", err)
	}
	defer userResp.Body.Close()

	if userResp.StatusCode != http.StatusOK {
		log.Fatalf("GitHub API error: %s", userResp.Status)
	}

	var userData GitHubUser
	if err := json.NewDecoder(userResp.Body).Decode(&userData); err != nil {
		log.Fatalf("Error decoding user data: %v", err)
	}

	// Fetch repositories
	repoReq, err := http.NewRequest("GET", 
		fmt.Sprintf("https://api.github.com/users/%s/repos?sort=updated&per_page=15", username), nil)
	if err != nil {
		log.Fatalf("Error creating repo request: %v", err)
	}
	
	repoReq.Header.Add("Authorization", "Bearer "+token)
	repoReq.Header.Add("Accept", "application/vnd.github.v3+json")
	
	repoResp, err := client.Do(repoReq)
	if err != nil {
		log.Fatalf("Error fetching repositories: %v", err)
	}
	defer repoResp.Body.Close()

	var repos []Repository
	if err := json.NewDecoder(repoResp.Body).Decode(&repos); err != nil {
		log.Fatalf("Error decoding repository data: %v", err)
	}

	return &userData, repos
}

func generateProfileAnalysis(ctx context.Context, client *genai.Client, user *GitHubUser, repos []Repository) *ProfileAnalysis {
    model := client.GenerativeModel("gemini-pro")
    
    // Prepare prompt for Gemini
    prompt := fmt.Sprintf(`Analyze this GitHub profile and provide a detailed personality assessment:

Profile Information:
- Name: %s
- Bio: %s
- Company: %s
- Location: %s
- Account Stats: %d followers, %d following, %d public repos
- Member since: %s

Recent Repository Analysis:
%s

Please provide:
1. Developer Personality Type (be creative and specific)
2. Key Technical Strengths (3-4 bullet points)
3. Areas for Profile Enhancement (2-3 points)
4. Specific Recommendations for Improvement
5. Primary Technology Stack
6. Activity Level Assessment

Format the response in clear sections with headers.`,
        user.Name, user.Bio, user.Company, user.Location,
        user.Followers, user.Following, user.PublicRepos,
        user.CreatedAt.Format("January 2006"),
        formatReposForPrompt(repos))

    response, err := model.GenerateContent(ctx, genai.Text(prompt))
    if err != nil {
        log.Fatalf("Error generating analysis: %v", err)
    }

    // Check if we have a response
    if response == nil || len(response.Candidates) == 0 {
        log.Fatal("No response received from Gemini")
    }

    // Get the text from the response
    var responseText string
    for _, part := range response.Candidates[0].Content.Parts {
        if textPart, ok := part.(genai.Text); ok {
            responseText += string(textPart)
        }
    }

    if responseText == "" {
        log.Fatal("No text content in Gemini response")
    }

	fmt.Println(responseText)
    return parseGeminiResponse(responseText)
}


func formatReposForPrompt(repos []Repository) string {
	var repoStrings []string
	for _, repo := range repos {
		if !repo.Fork {
			repoStrings = append(repoStrings, fmt.Sprintf("- %s (%s): %s [%d stars]", 
				repo.Name, repo.Language, repo.Description, repo.Stars))
		}
	}
	return strings.Join(repoStrings, "\n")
}

func parseGeminiResponse(response string) *ProfileAnalysis {
	analysis := &ProfileAnalysis{
		Strengths:   make([]string, 0),
		Areas:       make([]string, 0),
		Suggestions: make([]string, 0),
		TechStack:   make([]string, 0),
	}

	lines := strings.Split(response, "\n")
	currentSection := ""
	
	for _, line := range lines {
		line = strings.TrimSpace(line)
		
		switch {
		case strings.HasPrefix(line, "Personality Type:"):
			analysis.PersonalityType = strings.TrimPrefix(line, "Personality Type:")
			currentSection = "personality"
		case strings.HasPrefix(line, "Technical Strengths:"):
			currentSection = "strengths"
		case strings.HasPrefix(line, "Areas for Enhancement:"):
			currentSection = "areas"
		case strings.HasPrefix(line, "Recommendations:"):
			currentSection = "suggestions"
		case strings.HasPrefix(line, "Technology Stack:"):
			currentSection = "tech"
		case strings.HasPrefix(line, "Activity Level:"):
			analysis.ActivityLevel = strings.TrimPrefix(line, "Activity Level:")
			currentSection = "activity"
		case strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "• "):
			line = strings.TrimPrefix(strings.TrimPrefix(line, "- "), "• ")
			switch currentSection {
			case "strengths":
				analysis.Strengths = append(analysis.Strengths, line)
			case "areas":
				analysis.Areas = append(analysis.Areas, line)
			case "suggestions":
				analysis.Suggestions = append(analysis.Suggestions, line)
			case "tech":
				analysis.TechStack = append(analysis.TechStack, line)
			}
		}
	}
	
	return analysis
}

func printAnalysis(user *GitHubUser, analysis *ProfileAnalysis) {
	fmt.Printf("\n%s\n", strings.Repeat("=", 60))
	fmt.Printf("Profile Analysis for %s\n", user.Name)
	fmt.Printf("%s\n\n", strings.Repeat("=", 60))

	fmt.Printf("🎭 Developer Personality Type:\n%s\n\n", 
		strings.TrimSpace(analysis.PersonalityType))

	fmt.Printf("💪 Technical Strengths:\n")
	for _, strength := range analysis.Strengths {
		fmt.Printf("  • %s\n", strength)
	}
	fmt.Println()

	fmt.Printf("🔧 Primary Tech Stack:\n")
	for _, tech := range analysis.TechStack {
		fmt.Printf("  • %s\n", tech)
	}
	fmt.Println()

	fmt.Printf("📈 Areas for Enhancement:\n")
	for _, area := range analysis.Areas {
		fmt.Printf("  • %s\n", area)
	}
	fmt.Println()

	fmt.Printf("💡 Recommendations:\n")
	for _, suggestion := range analysis.Suggestions {
		fmt.Printf("  • %s\n", suggestion)
	}
	fmt.Println()

	fmt.Printf("📊 Activity Level: %s\n", analysis.ActivityLevel)
	fmt.Printf("%s\n", strings.Repeat("=", 60))
}

func getUserInput() string {
    var username string
    
    fmt.Println("\n=== GitHub Profile Analyzer ===")
    fmt.Println("This tool will analyze any GitHub profile and provide personality insights.")
    
    for {
        fmt.Print("\nEnter GitHub username to analyze (e.g., torvalds): ")
        fmt.Scanln(&username)
        
        // Remove any whitespace
        username = strings.TrimSpace(username)
        
        if username != "" {
            // Basic validation - GitHub usernames can't contain spaces
            if !strings.Contains(username, " ") {
                return username
            }
            fmt.Println("Invalid username: GitHub usernames cannot contain spaces.")
        } else {
            fmt.Println("Username cannot be empty. Please try again.")
        }
    }
}

func main() {
    // Load environment variables from .env file
    loadEnv()

    // Get environment variables
    githubToken := os.Getenv("GITHUB_TOKEN")
    geminiKey := os.Getenv("GEMINI_API_KEY")

    if githubToken == "" || geminiKey == "" {
        log.Fatal("Please set GITHUB_TOKEN and GEMINI_API_KEY in your .env file")
    }

    // Get GitHub username using the improved input function
    username := getUserInput()

    // Initialize Gemini client
    ctx := context.Background()
    client, err := genai.NewClient(ctx, option.WithAPIKey(geminiKey))
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    fmt.Printf("\nAnalyzing GitHub profile for %s...\n", username)

    // Fetch GitHub profile data
    userData, repos := fetchGitHubData(username, githubToken)
    
    // Generate profile analysis
    analysis := generateProfileAnalysis(ctx, client, userData, repos)
    
    // Print results
    printAnalysis(userData, analysis)
}