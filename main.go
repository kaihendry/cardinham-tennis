package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

type CalendarConfig struct {
	GoogleCalendarID  string            `json:"google_calendar_id"`
	CredentialsFile   string            `json:"credentials_file"`
	TokenFile         string            `json:"token_file"`
	UtilizationConfig UtilizationConfig `json:"utilization_config"`
}

type UtilizationConfig struct {
	StartHour       int  `json:"start_hour"`        // Default: 6 (6 AM)
	EndHour         int  `json:"end_hour"`          // Default: 18 (6 PM)
	ShowDailyStats  bool `json:"show_daily_stats"`  // Default: true
	ShowWeeklyStats bool `json:"show_weekly_stats"` // Default: true
}

type Booking struct {
	Title     string
	StartTime time.Time
	EndTime   time.Time
	Duration  time.Duration
}

type DayStats struct {
	Date        time.Time
	Bookings    []Booking
	TotalHours  float64
	Utilization float64
}

type WeekStats struct {
	WeekStart   time.Time
	WeekEnd     time.Time
	TotalHours  float64
	Utilization float64
	Days        []DayStats
}

func main() {
	// Load configuration
	config := loadCalendarConfig()

	// Create OAuth2 config
	ctx := context.Background()
	credentials, err := os.ReadFile(config.CredentialsFile)
	if err != nil {
		log.Fatalf("Unable to read credentials file: %v", err)
	}

	oauthConfig, err := google.ConfigFromJSON(credentials, calendar.CalendarReadonlyScope)
	if err != nil {
		log.Fatalf("Unable to parse client secret file to config: %v", err)
	}

	// Load or create token
	tok := loadCalendarToken(config.TokenFile, oauthConfig, ctx)

	// Create calendar service
	srv, err := calendar.NewService(ctx, option.WithHTTPClient(oauthConfig.Client(ctx, tok)))
	if err != nil {
		log.Fatalf("Unable to retrieve Calendar client: %v", err)
	}

	// Get events for the next 30 days
	now := time.Now()
	endTime := now.AddDate(0, 0, 30)

	events, err := srv.Events.List(config.GoogleCalendarID).
		ShowDeleted(false).
		SingleEvents(true).
		OrderBy("startTime").
		TimeMin(now.Format(time.RFC3339)).
		TimeMax(endTime.Format(time.RFC3339)).
		MaxResults(100).
		Do()

	if err != nil {
		log.Fatalf("Unable to retrieve events: %v", err)
	}

	fmt.Printf("Calendar Analysis for the next 30 days:\n")
	fmt.Printf("Calendar: %s\n", config.GoogleCalendarID)
	fmt.Printf("Found %d events\n\n", len(events.Items))

	if len(events.Items) == 0 {
		fmt.Println("No upcoming events found.")
		return
	}

	// Parse events into bookings
	bookings := parseBookings(events.Items)

	// Display individual bookings with hours
	displayBookings(bookings)

	// Calculate and display utilization statistics
	if config.UtilizationConfig.ShowDailyStats {
		dailyStats := calculateDailyStats(bookings, config.UtilizationConfig)
		displayDailyStats(dailyStats, config.UtilizationConfig)
	}

	if config.UtilizationConfig.ShowWeeklyStats {
		weeklyStats := calculateWeeklyStats(bookings, config.UtilizationConfig)
		displayWeeklyStats(weeklyStats, config.UtilizationConfig)
	}
}

func parseBookings(events []*calendar.Event) []Booking {
	var bookings []Booking

	for _, item := range events {
		if item.Start == nil || item.End == nil {
			continue
		}

		var startTime, endTime time.Time
		var err error

		// Parse start time
		if item.Start.DateTime != "" {
			startTime, err = time.Parse(time.RFC3339, item.Start.DateTime)
		} else {
			startTime, err = time.Parse("2006-01-02", item.Start.Date)
		}
		if err != nil {
			continue
		}

		// Parse end time
		if item.End.DateTime != "" {
			endTime, err = time.Parse(time.RFC3339, item.End.DateTime)
		} else {
			endTime, err = time.Parse("2006-01-02", item.End.Date)
		}
		if err != nil {
			continue
		}

		title := item.Summary
		if title == "" {
			title = "(No title)"
		}

		duration := endTime.Sub(startTime)

		bookings = append(bookings, Booking{
			Title:     title,
			StartTime: startTime,
			EndTime:   endTime,
			Duration:  duration,
		})
	}

	return bookings
}

func displayBookings(bookings []Booking) {
	fmt.Printf("=== Individual Bookings ===\n")
	for _, booking := range bookings {
		timeStr := booking.StartTime.Format("Mon Jan 2 15:04")
		if booking.StartTime.Hour() == 0 && booking.StartTime.Minute() == 0 {
			timeStr = booking.StartTime.Format("Mon Jan 2 (all day)")
		}

		hours := booking.Duration.Hours()
		fmt.Printf("%s - %s (%.1f hours)\n", timeStr, booking.Title, hours)
	}
	fmt.Println()
}

func calculateDailyStats(bookings []Booking, config UtilizationConfig) []DayStats {
	// Group bookings by day
	dayMap := make(map[string][]Booking)

	for _, booking := range bookings {
		dateKey := booking.StartTime.Format("2006-01-02")
		dayMap[dateKey] = append(dayMap[dateKey], booking)
	}

	var dailyStats []DayStats

	for dateKey, dayBookings := range dayMap {
		date, _ := time.Parse("2006-01-02", dateKey)

		// Calculate total hours for the day
		var totalHours float64
		for _, booking := range dayBookings {
			// Only count hours within the facility's operating hours
			bookingHours := calculateBookingHoursInRange(booking, config.StartHour, config.EndHour)
			totalHours += bookingHours
		}

		// Calculate utilization percentage
		availableHours := float64(config.EndHour - config.StartHour)
		utilization := (totalHours / availableHours) * 100

		dailyStats = append(dailyStats, DayStats{
			Date:        date,
			Bookings:    dayBookings,
			TotalHours:  totalHours,
			Utilization: utilization,
		})
	}

	return dailyStats
}

func calculateBookingHoursInRange(booking Booking, startHour, endHour int) float64 {
	// If it's an all-day event, count full operating hours
	if booking.StartTime.Hour() == 0 && booking.StartTime.Minute() == 0 {
		return float64(endHour - startHour)
	}

	// Calculate the effective start and end times within operating hours
	effectiveStart := booking.StartTime
	effectiveEnd := booking.EndTime

	// Adjust start time if it's before operating hours
	if effectiveStart.Hour() < startHour {
		effectiveStart = time.Date(effectiveStart.Year(), effectiveStart.Month(), effectiveStart.Day(), startHour, 0, 0, 0, effectiveStart.Location())
	}

	// Adjust end time if it's after operating hours
	if effectiveEnd.Hour() > endHour {
		effectiveEnd = time.Date(effectiveEnd.Year(), effectiveEnd.Month(), effectiveEnd.Day(), endHour, 0, 0, 0, effectiveEnd.Location())
	}

	// Calculate duration in hours
	duration := effectiveEnd.Sub(effectiveStart)
	hours := duration.Hours()

	// Ensure we don't return negative hours
	if hours < 0 {
		return 0
	}

	return hours
}

func displayDailyStats(dailyStats []DayStats, config UtilizationConfig) {
	hoursPerDay := config.EndHour - config.StartHour
	fmt.Printf("=== Daily Utilization Statistics ===\n")
	fmt.Printf("Operating Hours: %d:00 - %d:00 (%d hours/day)\n\n", config.StartHour, config.EndHour, hoursPerDay)

	for _, day := range dailyStats {
		fmt.Printf("%s: %.1f hours (%.1f%% utilization)\n",
			day.Date.Format("Mon Jan 2"),
			day.TotalHours,
			day.Utilization)
	}
	fmt.Println()
}

func calculateWeeklyStats(bookings []Booking, config UtilizationConfig) []WeekStats {
	// Group bookings by week
	weekMap := make(map[string][]Booking)

	for _, booking := range bookings {
		// Get the start of the week (Monday)
		weekStart := getWeekStart(booking.StartTime)
		weekKey := weekStart.Format("2006-01-02")
		weekMap[weekKey] = append(weekMap[weekKey], booking)
	}

	var weeklyStats []WeekStats

	for weekKey, weekBookings := range weekMap {
		weekStart, _ := time.Parse("2006-01-02", weekKey)
		weekEnd := weekStart.AddDate(0, 0, 6)

		// Calculate daily stats for this week
		dailyStats := calculateDailyStats(weekBookings, config)

		// Calculate total hours for the week
		var totalHours float64
		for _, day := range dailyStats {
			totalHours += day.TotalHours
		}

		// Calculate utilization percentage (7 days * hours per day)
		availableHours := float64(7 * (config.EndHour - config.StartHour))
		utilization := (totalHours / availableHours) * 100

		weeklyStats = append(weeklyStats, WeekStats{
			WeekStart:   weekStart,
			WeekEnd:     weekEnd,
			TotalHours:  totalHours,
			Utilization: utilization,
			Days:        dailyStats,
		})
	}

	return weeklyStats
}

func getWeekStart(date time.Time) time.Time {
	// Get Monday of the week
	weekday := date.Weekday()
	daysToSubtract := int(weekday - time.Monday)
	if daysToSubtract < 0 {
		daysToSubtract += 7
	}
	return date.AddDate(0, 0, -daysToSubtract)
}

func displayWeeklyStats(weeklyStats []WeekStats, config UtilizationConfig) {
	hoursPerDay := config.EndHour - config.StartHour
	hoursPerWeek := 7 * hoursPerDay
	fmt.Printf("=== Weekly Utilization Statistics ===\n")
	fmt.Printf("Operating Hours: %d:00 - %d:00 (%d hours/day, %d hours/week)\n\n",
		config.StartHour, config.EndHour, hoursPerDay, hoursPerWeek)

	for _, week := range weeklyStats {
		fmt.Printf("Week of %s - %s: %.1f hours (%.1f%% utilization)\n",
			week.WeekStart.Format("Jan 2"),
			week.WeekEnd.Format("Jan 2"),
			week.TotalHours,
			week.Utilization)

		// Show daily breakdown for this week
		for _, day := range week.Days {
			fmt.Printf("  %s: %.1f hours (%.1f%%)\n",
				day.Date.Format("Mon Jan 2"),
				day.TotalHours,
				day.Utilization)
		}
		fmt.Println()
	}
}

func loadCalendarConfig() CalendarConfig {
	// Try to load from config.json first (look in parent directory)
	if _, err := os.Stat("../config.json"); err == nil {
		data, err := os.ReadFile("../config.json")
		if err == nil {
			var config CalendarConfig
			if json.Unmarshal(data, &config) == nil {
				// Set defaults for utilization config if not provided
				if config.UtilizationConfig.StartHour == 0 {
					config.UtilizationConfig.StartHour = 6
				}
				if config.UtilizationConfig.EndHour == 0 {
					config.UtilizationConfig.EndHour = 18
				}
				if !config.UtilizationConfig.ShowDailyStats && !config.UtilizationConfig.ShowWeeklyStats {
					config.UtilizationConfig.ShowDailyStats = true
					config.UtilizationConfig.ShowWeeklyStats = true
				}
				return config
			}
		}
	}

	// Fallback to environment variables or defaults
	calendarID := os.Getenv("GOOGLE_CALENDAR_ID")
	if calendarID == "" {
		calendarID = "cardinhamsports@gmail.com"
	}

	credentialsFile := os.Getenv("GOOGLE_CREDENTIALS_FILE")
	if credentialsFile == "" {
		credentialsFile = "credentials.json"
	}

	tokenFile := os.Getenv("GOOGLE_TOKEN_FILE")
	if tokenFile == "" {
		tokenFile = "token.json"
	}

	// Parse utilization config from environment
	startHour := 6 // Default 6 AM
	if startStr := os.Getenv("UTILIZATION_START_HOUR"); startStr != "" {
		if start, err := fmt.Sscanf(startStr, "%d", &startHour); err != nil || start != 1 {
			startHour = 6
		}
	}

	endHour := 18 // Default 6 PM
	if endStr := os.Getenv("UTILIZATION_END_HOUR"); endStr != "" {
		if end, err := fmt.Sscanf(endStr, "%d", &endHour); err != nil || end != 1 {
			endHour = 18
		}
	}

	return CalendarConfig{
		GoogleCalendarID: calendarID,
		CredentialsFile:  credentialsFile,
		TokenFile:        tokenFile,
		UtilizationConfig: UtilizationConfig{
			StartHour:       startHour,
			EndHour:         endHour,
			ShowDailyStats:  true,
			ShowWeeklyStats: true,
		},
	}
}

func loadCalendarToken(tokenFile string, config *oauth2.Config, ctx context.Context) *oauth2.Token {
	// Try to load existing token
	if _, err := os.Stat(tokenFile); err == nil {
		f, err := os.Open(tokenFile)
		if err == nil {
			defer f.Close()
			tok := &oauth2.Token{}
			if err := json.NewDecoder(f).Decode(tok); err == nil {
				return tok
			}
		}
	}

	// If no token exists, get a new one
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Go to the following link in your browser:\n%v\n\n", authURL)
	fmt.Print("Enter the authorization code: ")

	var authCode string
	if _, err := fmt.Scan(&authCode); err != nil {
		log.Fatalf("Unable to read authorization code: %v", err)
	}

	tok, err := config.Exchange(ctx, authCode)
	if err != nil {
		log.Fatalf("Unable to retrieve token from web: %v", err)
	}

	// Save the token for future use
	saveCalendarToken(tokenFile, tok)
	return tok
}

func saveCalendarToken(path string, token *oauth2.Token) {
	fmt.Printf("Saving credential file to: %s\n", path)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Fatalf("Unable to cache oauth token: %v", err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(token)
}
