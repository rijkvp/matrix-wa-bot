package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mdp/qrterminal/v3"
	qrcode "github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"

	matrix "github.com/matrix-org/gomatrix"
)

type Config struct {
	HomeserverURL string
	UserID        string
	AccessToken   string
	RoomID        string
	DbPath        string
}

type WhatsAppBot struct {
	cfg      *Config
	waClient *whatsmeow.Client
	mxClient *matrix.Client
}

func main() {
	cfg := Config{
		HomeserverURL: os.Getenv("HOMESERVER_URL"),
		UserID:        os.Getenv("USER_ID"),
		AccessToken:   os.Getenv("ACCESS_TOKEN"),
		RoomID:        os.Getenv("ROOM_ID"),
		DbPath:        os.Getenv("DB_PATH"),
	}

	bot := NewBot(&cfg)
	bot.Run()
}

func NewBot(cfg *Config) WhatsAppBot {
	log.Println("Logging in to matrix..")
	mxClient, err := matrix.NewClient(cfg.HomeserverURL, cfg.UserID, cfg.AccessToken)
	if err != nil {
		log.Fatalln("Failed to login to matrix:", err)
	}

	dbLog := waLog.Stdout("Database", "INFO", true)
	container, err := sqlstore.New("sqlite3", "file:"+cfg.DbPath+"?_foreign_keys=on", dbLog)
	if err != nil {
		log.Fatalln("Failed to create database", err)
	}
	deviceStore, err := container.GetFirstDevice()
	if err != nil {
		log.Fatalln("Failed to get db device", err)
	}
	clientLog := waLog.Stdout("Client", "INFO", true)
	waClient := whatsmeow.NewClient(deviceStore, clientLog)

	return WhatsAppBot{cfg, waClient, mxClient}
}

func (bot *WhatsAppBot) Run() {
	bot.waClient.AddEventHandler(bot.eventHandler)

	if bot.waClient.Store.ID == nil {
		log.Println("Connecting to WhatsApp...")
		qrChan, _ := bot.waClient.GetQRChannel(context.Background())
		err := bot.waClient.Connect()
		if err != nil {
			panic(err)
		}
		for evt := range qrChan {
			if evt.Event == "code" {
				log.Println("Scan the QR code!")
				qrterminal.Generate(evt.Code, qrterminal.L, os.Stdout)
				qrcode.WriteFile(evt.Code, qrcode.Medium, 1024, "/tmp/wabot-qrcode.png")
			} else {
				log.Println("Login event:", evt.Event)
			}
		}
	} else {
		log.Println("Connecting to existing account...")
		err := bot.waClient.Connect()
		if err != nil {
			panic(err)
		}
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	log.Println("Disconnecting from  WhatsApp..")
	bot.waClient.Disconnect()
	log.Println("Closed whatsapp session.")
}

func (w *WhatsAppBot) eventHandler(evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		if v.Info.IsFromMe {
			return
		}

		phoneNumber := v.Info.MessageSource.Sender.User
		conversation := v.Message.GetConversation()
		var username string
		var context string

		contact, err := w.waClient.Store.Contacts.GetContact(v.Info.Sender)
		if err != nil {
			log.Println("Failed to get user info:", err)
		}
		if contact.Found {
			username = fmt.Sprintf("%s (+%s)", contact.FullName, phoneNumber)
		} else {
			username = fmt.Sprintf("+%s", phoneNumber)
		}
		if v.Info.IsGroup {
			group, err := w.waClient.GetGroupInfo(v.Info.Chat)
			if err != nil {
				log.Println("Failed to get group info:", err)
				context = "Unkown group"
			} else {
				context = group.Name
			}
		} else {
			context = "DM " + phoneNumber
		}
		w.notify(context, username, conversation)
	case *events.Presence:
		log.Printf("[WA PRESENCE] %s: %s %v\n", v.From.User, v.LastSeen, v.Unavailable)
	}
}

func (w *WhatsAppBot) notify(context string, username string, conversation string) {
	msg := fmt.Sprintf("[%s] %s: %v", context, username, conversation)
	log.Println("[NOTIFY]", msg)
	w.sendMessage(msg)
}

func (w *WhatsAppBot) sendMessage(msg string) {
	_, err := w.mxClient.SendFormattedText(w.cfg.RoomID, msg, "<b>"+msg+"</b>")
	if err != nil {
		log.Println("ERROR: Failed to send matrix message:", err)
	}
}
