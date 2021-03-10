package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"

	"github.com/bwmarrin/discordgo"
)

var (
	GuildID  = flag.String("g", "", "Guild ID")
	BotToken = flag.String("t", "", "Bot token")
)

var session *discordgo.Session

func init() { flag.Parse() }

func init() {
	var err error
	session, err = discordgo.New("Bot " + *BotToken)
	if err != nil {
		log.Fatalf("Missing bot parameters: %v", err)
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
	}
)

func init() {
	session.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if h, ok := commandHandlers[i.Data.Name]; ok {
			h(s, i)
		}
	})
}

func main() {
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
}
