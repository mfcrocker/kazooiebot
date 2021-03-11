package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	firebase "firebase.google.com/go"
	"github.com/bwmarrin/discordgo"
	"github.com/robfig/cron/v3"
	"google.golang.org/api/iterator"
)

var (
	GuildID  = flag.String("g", "", "Guild ID")
	BotToken = flag.String("t", "", "Bot token")
)

var session *discordgo.Session
var ctx context.Context
var client *firestore.Client

func init() { flag.Parse() }

func init() {
	var err error
	session, err = discordgo.New("Bot " + *BotToken)
	if err != nil {
		log.Fatalf("Missing bot parameters: %v", err)
	}

	ctx = context.Background()
	conf := &firebase.Config{ProjectID: "kazooie-bot"}
	app, err := firebase.NewApp(ctx, conf)
	if err != nil {
		log.Fatalf("Couldn't connect to Firestore: %v", err)
	}

	client, err = app.Firestore(ctx)
	if err != nil {
		log.Fatalf("Couldn't connect to Firestore: %v", err)
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
					Name:        "date",
					Description: "The date to remind you, in YYYY-MM-DD format",
					Required:    true,
				},
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "time",
					Description: "The time to remind you, in 24-hour HH:MM format (use UTC)",
					Required:    true,
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
			if !strings.Contains(i.Data.Options[0].StringValue(), ":") {
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionApplicationCommandResponseData{
						Flags: 64,
					},
				})
			} else {
				emojiID := strings.TrimSuffix(strings.Split(i.Data.Options[0].StringValue(), ":")[2], ">")
				emojiURI := "https://cdn.discordapp.com/emojis/" + emojiID + ".gif?v=1"
				// Check if its a gif or not
				resp, err := http.Head(emojiURI)
				if err != nil || resp.StatusCode != http.StatusOK {
					emojiURI = emojiURI[:len(emojiURI)-8] + ".png?v=1"
				}
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionApplicationCommandResponseData{
						Content: emojiURI,
					},
				})
			}
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
			reminderDate := i.Data.Options[1].StringValue() + "T" + i.Data.Options[2].StringValue() + ".000Z"
			reminderTimestamp, err := time.Parse(time.RFC3339, reminderDate)
			if err != nil {
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionApplicationCommandResponseData{
						Content: "That's not the right date or time format; use YYYY-MM-DD HH:MM in 24 hour clock, UTC",
					},
				})
				return
			}

			_, _, err = client.Collection("reminders").Add(ctx, map[string]interface{}{
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
				return
			}

			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionApplicationCommandResponseData{
					Content: "Okay, I've set a reminder up to remind you of " + i.Data.Options[0].StringValue(),
				},
			})
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
	iter := client.Collection("reminders").Where("date", "<", time.Now()).Documents(ctx)
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
	c := cron.New()
	c.AddFunc("@every 1m", func() { checkReminders() })
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
	defer client.Close()

	stop := make(chan os.Signal)
	signal.Notify(stop, os.Interrupt)
	<-stop
	log.Println("Shutting down bird asses")
	c.Stop()
}
