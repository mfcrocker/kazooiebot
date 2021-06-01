package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	firebase "firebase.google.com/go"
	"github.com/bwmarrin/discordgo"
	"github.com/robfig/cron/v3"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"
)

var (
	GuildID      = flag.String("g", "", "Guild ID")
	BotToken     = flag.String("t", "", "Bot token")
	GCPProject   = flag.String("p", "", "GCP Project")
	YouTubeToken = flag.String("y", "", "YouTube token")
)

var session *discordgo.Session
var ctx context.Context
var firestoreClient *firestore.Client
var youtubeClient *youtube.Service

const prettyDateFormat = "January 2, 2006"

type month struct {
	StartTime time.Time `json:"start_time"`
	Days      []day     `json:"days"`
}

type day struct {
	Day    int    `json:"day"`
	Prompt string `json:"prompt"`
}

func init() { flag.Parse() }

func init() {
	var err error
	session, err = discordgo.New("Bot " + *BotToken)
	if err != nil {
		log.Fatalf("Missing bot parameters: %v", err)
	}

	ctx = context.Background()
	conf := &firebase.Config{ProjectID: *GCPProject}
	app, err := firebase.NewApp(ctx, conf)
	if err != nil {
		log.Printf("Couldn't connect to Firestore, so many commands will not work: %v", err)
		return
	}

	firestoreClient, err = app.Firestore(ctx)
	if err != nil {
		log.Printf("Couldn't connect to Firestore, so many commands will not work: %v", err)
		return
	}

	data, err := ioutil.ReadFile("client_secret.json")
	if err != nil {
		log.Printf("Couldn't find or decode client_secret.json; YouTube integration will fail: %v", err)
		return
	}
	config, err := google.ConfigFromJSON(data, "https://www.googleapis.com/auth/youtubepartner")
	if err != nil {
		log.Printf("Couldn't find or decode client_secret.json; YouTube integration will fail: %v", err)
		return
	}

	if *YouTubeToken == "" {
		url := config.AuthCodeURL("state", oauth2.AccessTypeOffline)
		fmt.Printf("Please visit the URL for YouTube auth, then restart this with the -y flag: %v. YouTube integration will fail without the flag.", url)
		return
	}

	token, err := config.Exchange(ctx, *YouTubeToken)
	if err != nil {
		log.Printf("Couldn't connect to YouTube; YouTube integration will fail: %v", err)
		return
	}

	youtubeClient, err = youtube.NewService(ctx, option.WithTokenSource(config.TokenSource(ctx, token)))
	if err != nil {
		log.Printf("Couldn't connect to YouTube; YouTube integration will fail: %v", err)
		return
	}
}

var (
	commands = []*discordgo.ApplicationCommand{
		{
			Name:        "birdass",
			Description: "Just birdass",
		},
		{
			Name:        "addrole",
			Description: "Add a role to yourself, eg pronouns or colours",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionRole,
					Name:        "role",
					Description: "The role to add",
					Required:    true,
				},
			},
		},
		{
			Name:        "removerole",
			Description: "Remove a role from yourself",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionRole,
					Name:        "rolerole",
					Description: "The role to add",
					Required:    true,
				},
			},
		},
		{
			Name:        "bigemoji",
			Description: "Emoji, but T H I C C",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "emoji",
					Description: "The emoji to biggify",
					Required:    true,
				},
			},
		},
		{
			Name:        "bogart",
			Description: "bogart",
		},
		{
			Name:        "reminder",
			Description: "Set a reminder for yourself",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "reminder",
					Description: "Thing to remind you of",
					Required:    true,
				},
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "when",
					Description: "When should I remind you? (format: 5d3h30m)",
					Required:    true,
				},
			},
		},
		{
			Name:        "suggestion",
			Description: "Make a feature request for this bot of bird and ass",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "suggestion",
					Description: "What do you want to see implemented?",
					Required:    true,
				},
			},
		},
		{
			Name:        "utc",
			Description: "Gets the current time in UTC",
		},
		{
			Name:        "musicsetup",
			Description: "Sets up a music month - only works for mfcrocker",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "file",
					Description: "A URL to a text file",
					Required:    true,
				},
			},
		},
		{
			Name:        "musicmonth",
			Description: "Get the current music month, if any",
		},
		{
			Name:        "musicprompt",
			Description: "Get the prompt for a music month",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionInteger,
					Name:        "day",
					Description: "The day to retrieve (gets today if not provided)",
					Required:    false,
				},
			},
		},
		{
			Name:        "music",
			Description: "Set your song for a prompt",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "song",
					Description: "The song to submit, ideally as a YouTube link",
					Required:    true,
				},
				{
					Type:        discordgo.ApplicationCommandOptionInteger,
					Name:        "day",
					Description: "The day to set (sets today if not provided)",
					Required:    false,
				},
			},
		},
		{
			Name:        "musicplaylist",
			Description: "Create/retrieve a playlist of your songs for the most recent music month",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionBoolean,
					Name:        "mine",
					Description: "Whether you want the whole server's songs or just your own",
					Required:    true,
				},
				{
					Type:        discordgo.ApplicationCommandOptionInteger,
					Name:        "day",
					Description: "Which day's songs to retrieve (returns every day if empty)",
					Required:    false,
				},
			},
		},
	}

	commandHandlers = map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate){
		"birdass": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionApplicationCommandResponseData{
					Content: "just birdass",
				},
			})
		},
		"addrole": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionApplicationCommandResponseData{
					Flags: 64,
				},
			})
			s.GuildMemberRoleAdd(i.GuildID, i.Member.User.ID, i.Data.Options[0].RoleValue(nil, "").ID)
		},
		"removerole": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionApplicationCommandResponseData{
					Flags: 64,
				},
			})
			s.GuildMemberRoleRemove(i.GuildID, i.Member.User.ID, i.Data.Options[0].RoleValue(nil, "").ID)
		},
		"bigemoji": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			valid, _ := regexp.MatchString(`<a?:\w+:\d+>`, i.Data.Options[0].StringValue())
			if !valid {
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionApplicationCommandResponseData{
						Flags: 64,
					},
				})
				return
			}
			emojiID := strings.TrimSuffix(strings.Split(i.Data.Options[0].StringValue(), ":")[2], ">")
			animated, _ := regexp.MatchString(`<a:\w+:\d+>`, i.Data.Options[0].StringValue())
			suffix := ".png?v=1"
			if animated {
				suffix = ".gif?v=1"
			}
			emojiURI := "https://cdn.discordapp.com/emojis/" + emojiID + suffix
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionApplicationCommandResponseData{
					Content: emojiURI,
				},
			})
		},
		"bogart": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionApplicationCommandResponseData{
					Content: "https://cdn.discordapp.com/emojis/721104351220727859.png?v=1",
				},
			})
		},
		"reminder": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			if firestoreClient == nil {
				// We're not connected to GCP, don't let them do this
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionApplicationCommandResponseData{
						Content: "I haven't been set up to allow reminders, please moan at whoever set me up",
					},
				})
				return
			}
			timeString := i.Data.Options[1].StringValue()
			offset := 0
			parseString := timeString
			if strings.Contains(timeString, "d") {
				parseString = strings.Split(timeString, "d")[0]
				days, err := strconv.Atoi(parseString)
				if err != nil {
					s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
						Type: discordgo.InteractionResponseChannelMessageWithSource,
						Data: &discordgo.InteractionApplicationCommandResponseData{
							Content: "That's not the right date or time format. Example: 5d3h30m for a reminder in 5 1/2 hours",
						},
					})
					return
				}
				offset += days * 24
				parseString = strings.Split(timeString, "d")[1]
			}

			parsedDuration, err := time.ParseDuration(parseString)
			parsedOffset, _ := time.ParseDuration(strconv.Itoa(offset) + "h")
			parsedDuration += parsedOffset
			if err != nil {
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionApplicationCommandResponseData{
						Content: "That's not the right date or time format. Example: 5d3h30m for a reminder in 5 1/2 hours",
					},
				})
				return
			}
			reminderTimestamp := time.Now().Add(parsedDuration)

			_, _, err = firestoreClient.Collection("reminders").Add(ctx, map[string]interface{}{
				"userID":   i.Member.User.ID,
				"reminder": i.Data.Options[0].StringValue(),
				"date":     reminderTimestamp,
			})

			if err != nil {
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionApplicationCommandResponseData{
						Content: "Something went wrong at my end so I didn't save your reminder",
					},
				})
				log.Printf("Error saving record to Firestore: %v", err)
				return
			}

			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionApplicationCommandResponseData{
					Content: "Okay, I've set a reminder up to remind you of " + i.Data.Options[0].StringValue(),
				},
			})
		},
		"suggestion": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionApplicationCommandResponseData{
					Flags: 64,
				},
			})
			channel, err := session.UserChannelCreate(fmt.Sprintf("%v", "147856569730596864"))
			if err != nil {
				fmt.Printf("Couldn't talk to user: %v", err)
			}
			_, err = session.ChannelMessageSend(channel.ID, "You've had a suggestion from "+i.Member.User.Username+": "+i.Data.Options[0].StringValue())
		},
		"utc": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionApplicationCommandResponseData{
					Content: "The current time is: " + time.Now().UTC().Format("15:04:05 MST Jan _2"),
				},
			})
		},
		"musicsetup": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			if firestoreClient == nil {
				// We're not connected to GCP, don't let them do this
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionApplicationCommandResponseData{
						Content: "I haven't been set up to allow music months, please moan at whoever set me up",
					},
				})
				return
			}
			if i.Member.User.ID != "147856569730596864" {
				// You ain't me
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionApplicationCommandResponseData{
						Content: "Please ask mfcrocker to set this up!",
					},
				})
				return
			}
			if !strings.HasSuffix(i.Data.Options[0].StringValue(), ".json") {
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionApplicationCommandResponseData{
						Content: "Give me a .json file",
					},
				})
				return
			}

			resp, err := http.Get(i.Data.Options[0].StringValue())
			if err != nil {
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionApplicationCommandResponseData{
						Content: "Couldn't get the file from the URL provided",
					},
				})
				return
			}
			defer resp.Body.Close()

			monthData, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionApplicationCommandResponseData{
						Content: "Error reading the file bytes",
					},
				})
				return
			}

			var musicMonth month
			err = json.Unmarshal(monthData, &musicMonth)
			if err != nil {
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionApplicationCommandResponseData{
						Content: "Invalid JSON",
					},
				})
				return
			}

			_, _, err = firestoreClient.Collection("musicmonth").Add(ctx, musicMonth)

			if err != nil {
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionApplicationCommandResponseData{
						Content: "Something went wrong at my end so I didn't save the month",
					},
				})
				log.Printf("Error saving record to Firestore: %v", err)
				return
			}

			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionApplicationCommandResponseData{
					Content: "Okay, I've set up a music month beginning on " + musicMonth.StartTime.Format(prettyDateFormat),
				},
			})
		},
		"musicmonth": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			if firestoreClient == nil {
				// We're not connected to GCP, don't let them do this
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionApplicationCommandResponseData{
						Content: "I haven't been set up to allow music months, please moan at whoever set me up",
					},
				})
				return
			}
			now := time.Now().UTC()
			// Give a couple of days grace on this - would normally be -now.Day() + 1
			currentMonthStart := now.AddDate(0, 0, -now.Day()-1)
			currentMonthEnd := now.AddDate(0, 1, -now.Day())
			iter := firestoreClient.Collection("musicmonth").Where("StartTime", ">", currentMonthStart).OrderBy("StartTime", firestore.Asc).Limit(1).Documents(ctx)
			docs, _ := iter.GetAll()
			if len(docs) == 0 {
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionApplicationCommandResponseData{
						Content: "No music month planned",
					},
				})
				return
			}

			var currentMonth month
			docs[0].DataTo(&currentMonth)
			var response strings.Builder

			if currentMonth.StartTime.After(currentMonthEnd) {
				response.WriteString("There's no current music month; the next begins on " + currentMonth.StartTime.Format(prettyDateFormat) + "\n")
			} else {
				response.WriteString("Current music month: \n")
			}
			response.WriteString("```")
			for _, day := range currentMonth.Days {
				response.WriteString(currentMonth.StartTime.Format("January") + " " + strconv.Itoa(day.Day) + ": " + day.Prompt + "\n")
			}
			response.WriteString("```")

			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionApplicationCommandResponseData{
					Content: response.String(),
				},
			})
		},
		"musicprompt": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			now := time.Now().UTC()
			day := now.Day()
			if len(i.Data.Options) > 0 {
				day = int(i.Data.Options[0].IntValue())
			}

			// Give a couple of days grace on this - would normally be -now.Day() + 1
			currentMonthStart := now.AddDate(0, 0, -now.Day()-1)
			currentMonthEnd := now.AddDate(0, 1, -now.Day())
			iter := firestoreClient.Collection("musicmonth").Where("StartTime", ">", currentMonthStart).Where("StartTime", "<", currentMonthEnd).OrderBy("StartTime", firestore.Asc).Limit(1).Documents(ctx)
			docs, _ := iter.GetAll()
			if len(docs) == 0 {
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionApplicationCommandResponseData{
						Content: "No currently active music month",
					},
				})
				return
			}
			var currentMonth month
			docs[0].DataTo(&currentMonth)
			for _, prompt := range currentMonth.Days {
				if prompt.Day == day {
					s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
						Type: discordgo.InteractionResponseChannelMessageWithSource,
						Data: &discordgo.InteractionApplicationCommandResponseData{
							Content: "Prompt for day " + strconv.Itoa(prompt.Day) + ": " + prompt.Prompt,
						},
					})
					return
				}
			}
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionApplicationCommandResponseData{
					Content: "No prompt found for day " + strconv.Itoa(day),
				},
			})
		},
		"music": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			now := time.Now().UTC()
			// Give a couple of days grace on this - would normally be -now.Day() + 1
			currentMonthStart := now.AddDate(0, 0, -now.Day()-1)
			currentMonthEnd := now.AddDate(0, 1, -now.Day())
			iter := firestoreClient.Collection("musicmonth").Where("StartTime", ">", currentMonthStart).Where("StartTime", "<", currentMonthEnd).OrderBy("StartTime", firestore.Asc).Limit(1).Documents(ctx)
			docs, _ := iter.GetAll()
			if len(docs) == 0 {
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionApplicationCommandResponseData{
						Content: "No currently active music month",
					},
				})
				return
			}

			var retrievedMonth month
			docs[0].DataTo(&retrievedMonth)
			monthName := retrievedMonth.StartTime.Format("Jan 2006")
			day := now.Day()
			if len(i.Data.Options) > 1 {
				day = int(i.Data.Options[1].IntValue())
			}

			var response strings.Builder

			iter = firestoreClient.Collection("music").Where("userID", "==", i.Member.User.ID).Where("month", "==", monthName).Where("day", "==", day).Documents(ctx)
			docs, _ = iter.GetAll()
			if len(docs) > 0 {
				response.WriteString("Replacing your old pick of " + docs[0].Data()["song"].(string) + "\n")
				docs[0].Ref.Delete(ctx)
			}

			firestoreClient.Collection("music").Add(ctx, map[string]interface{}{
				"userID": i.Member.User.ID,
				"month":  monthName,
				"day":    day,
				"song":   i.Data.Options[0].StringValue(),
			})

			response.WriteString("Submitting " + i.Data.Options[0].StringValue() + " for day " + strconv.Itoa(day))
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionApplicationCommandResponseData{
					Content: response.String(),
				},
			})
		},
		"musicplaylist": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			iter := firestoreClient.Collection("musicmonth").Where("StartTime", "<", time.Now().UTC()).OrderBy("StartTime", firestore.Desc).Limit(1).Documents(ctx)
			docs, _ := iter.GetAll()
			if len(docs) == 0 {
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionApplicationCommandResponseData{
						Content: "No music month past or present found",
					},
				})
				return
			}

			var retrievedMonth month
			docs[0].DataTo(&retrievedMonth)
			monthName := retrievedMonth.StartTime.Format("Jan 2006")

			if len(i.Data.Options) > 1 {
				day := int(i.Data.Options[1].IntValue())
				if i.Data.Options[0].BoolValue() {
					// Specific day, user only
					// Don't make a playlist for one song for one person!
					iter = firestoreClient.Collection("music").Where("userID", "==", i.Member.User.ID).Where("month", "==", monthName).Where("day", "==", day).Documents(ctx)
					docs, _ = iter.GetAll()
					if len(docs) > 0 {
						s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
							Type: discordgo.InteractionResponseChannelMessageWithSource,
							Data: &discordgo.InteractionApplicationCommandResponseData{
								Content: "Your pick for day " + strconv.Itoa(day) + " of " + monthName + " was " + docs[0].Data()["song"].(string),
							},
						})
						return
					} else {
						s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
							Type: discordgo.InteractionResponseChannelMessageWithSource,
							Data: &discordgo.InteractionApplicationCommandResponseData{
								Content: "I have no pick saved for you for day " + strconv.Itoa(day) + " of " + monthName,
							},
						})
						return
					}
				} else {
					// Specific day, whole server
					iter = firestoreClient.Collection("music").Where("month", "==", monthName).Where("day", "==", day).Documents(ctx)
					songDocs, _ := iter.GetAll()

					if len(songDocs) == 0 {
						s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
							Type: discordgo.InteractionResponseChannelMessageWithSource,
							Data: &discordgo.InteractionApplicationCommandResponseData{
								Content: "No-one has submitted any songs for day " + strconv.Itoa(day) + " of " + monthName,
							},
						})
						return
					}
					iter = firestoreClient.Collection("musicplaylists").Where("userID", "==", "").Where("month", "==", monthName).Where("day", "==", day).Documents(ctx)
					playlistDocs, _ := iter.GetAll()
					playlistID := ""
					if len(playlistDocs) == 0 {
						// Create a new playlist
						insertPlaylist := &youtube.Playlist{
							Snippet: &youtube.PlaylistSnippet{
								Title:       "Speedfriends Music Month: " + monthName + " Day " + strconv.Itoa(day),
								Description: "All the songs posted on day " + strconv.Itoa(day) + " of " + monthName + "'s music month in Speedfriends",
							},
							Status: &youtube.PlaylistStatus{PrivacyStatus: "unlisted"},
						}
						part := []string{"snippet", "status"}
						call := youtubeClient.Playlists.Insert(part, insertPlaylist)
						response, err := call.Do()
						if err != nil {
							s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
								Type: discordgo.InteractionResponseChannelMessageWithSource,
								Data: &discordgo.InteractionApplicationCommandResponseData{
									Content: "Error creating a playlist",
								},
							})
							log.Printf("Error creating a playlist: %v", err)
							return
						}
						firestoreClient.Collection("musicplaylists").Add(ctx, map[string]interface{}{
							"userID":     "",
							"month":      monthName,
							"day":        day,
							"playlistID": response.Id,
						})

						playlistID = response.Id
					} else {
						playlistID = playlistDocs[0].Data()["playlistID"].(string)
					}

					// Check all the songs on the playlist match the songs we have saved, and insert/delete as appropriate
					pageToken := ""
					var playlistVideos []*youtube.PlaylistItem
					for {
						part := []string{"contentDetails"}
						call := youtubeClient.PlaylistItems.List(part)
						call = call.PlaylistId(playlistID)
						if pageToken != "" {
							call = call.PageToken(pageToken)
						}
						response, err := call.Do()
						if err != nil {
							s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
								Type: discordgo.InteractionResponseChannelMessageWithSource,
								Data: &discordgo.InteractionApplicationCommandResponseData{
									Content: "Error retrieving a playlist",
								},
							})
							log.Printf("Error retrieving a playlist: %v", err)
							return
						}

						playlistVideos = append(playlistVideos, response.Items...)
						pageToken = response.NextPageToken
						if pageToken == "" {
							break
						}
					}

					// Check the songs we have our end are in the playlist and add if necessary
					for _, gcpsong := range songDocs {
						inPlaylist := false
						gcpID := ""
						if strings.Contains(gcpsong.Data()["song"].(string), "youtube") {
							gcpID = strings.Split(strings.Split(gcpsong.Data()["song"].(string), "=")[1], "&")[0]
						} else if strings.Contains(gcpsong.Data()["song"].(string), "youtu.be") {
							gcpID = strings.Split(gcpsong.Data()["song"].(string), "/")[3]
						} else {
							// Probably not YT
							continue
						}
						for _, ytsong := range playlistVideos {
							if gcpID == ytsong.ContentDetails.VideoId {
								inPlaylist = true
								break
							}
						}
						if !inPlaylist {
							part := []string{"snippet"}
							video := &youtube.PlaylistItem{
								Snippet: &youtube.PlaylistItemSnippet{
									PlaylistId: playlistID,
									ResourceId: &youtube.ResourceId{
										Kind:    "youtube#video",
										VideoId: gcpID,
									},
								},
							}
							call := youtubeClient.PlaylistItems.Insert(part, video)
							_, err := call.Do()
							if err != nil {
								s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
									Type: discordgo.InteractionResponseChannelMessageWithSource,
									Data: &discordgo.InteractionApplicationCommandResponseData{
										Content: "Error updating a playlist",
									},
								})
								log.Printf("Error updating a playlist: %v", err)
								return
							}
						}
					}

					// Check the songs we have on YouTube's end are in GCP and delete if necessary
					for _, ytsong := range playlistVideos {
						inGCP := false
						for _, gcpsong := range songDocs {
							if strings.Contains(gcpsong.Data()["song"].(string), ytsong.ContentDetails.VideoId) {
								inGCP = true
								break
							}
						}
						if !inGCP {
							call := youtubeClient.PlaylistItems.Delete(ytsong.Id)
							err := call.Do()
							if err != nil {
								s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
									Type: discordgo.InteractionResponseChannelMessageWithSource,
									Data: &discordgo.InteractionApplicationCommandResponseData{
										Content: "Error updating a playlist",
									},
								})
								log.Printf("Error updating a playlist: %v", err)
								return
							}
						}
					}
					s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
						Type: discordgo.InteractionResponseChannelMessageWithSource,
						Data: &discordgo.InteractionApplicationCommandResponseData{
							Content: "Playlist for " + monthName + " Day " + strconv.Itoa(day) + ": https://youtube.com/playlist?list=" + playlistID,
						},
					})
					return
				}
			} else {
				if i.Data.Options[0].BoolValue() {
					// Whole month, user only
				} else {
					// Whole month, whole server
				}
			}
		},
	}
)

func init() {
	session.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if h, ok := commandHandlers[i.Data.Name]; ok {
			h(s, i)
		}
	})
}

func checkReminders() {
	iter := firestoreClient.Collection("reminders").Where("date", "<", time.Now()).Documents(ctx)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			fmt.Printf("Something went wrong getting reminders on a cron: %v", err)
			break
		}
		channel, err := session.UserChannelCreate(fmt.Sprintf("%v", doc.Data()["userID"]))
		if err != nil {
			fmt.Printf("Couldn't talk to user: %v", err)
		}
		_, err = session.ChannelMessageSend(channel.ID, "Hi there! You asked me to remind you about "+fmt.Sprintf("%v", doc.Data()["reminder"])+" - this is that reminder!")
		if err != nil {
			fmt.Printf("Error trying to remind someone: %v", err)
		}

		doc.Ref.Delete(ctx)
	}
}

func main() {
	var c *cron.Cron
	if firestoreClient != nil {
		c := cron.New()
		c.AddFunc("@every 1m", func() { checkReminders() })
		c.Start()
		defer firestoreClient.Close()
	}
	session.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		log.Println("Ready to birdass")
	})
	err := session.Open()
	if err != nil {
		log.Fatalf("Couldn't connect to Discord: %v", err)
	}

	for _, v := range commands {
		_, err := session.ApplicationCommandCreate(session.State.User.ID, *GuildID, v)
		if err != nil {
			log.Fatalf("Couldn't create '%v' command: %v", v.Name, err)
		}
	}

	defer session.Close()

	stop := make(chan os.Signal)
	signal.Notify(stop, os.Interrupt)
	<-stop
	log.Println("Shutting down bird asses")
	if c != nil {
		c.Stop()
	}
}
