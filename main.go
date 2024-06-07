package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	_ "github.com/mattn/go-sqlite3" // Import the SQLite3 driver
	logrus "github.com/sirupsen/logrus"
)

var client *whatsmeow.Client

func main() {
	// Setup logging
	logrus.SetLevel(logrus.DebugLevel)

	// Setup database
	container, err := sqlstore.New("sqlite3", "file:whatsmeow.db?_foreign_keys=on", nil)
	if err != nil {
		log.Fatalf("Failed to create container: %v", err)
	}

	deviceStore, err := container.GetFirstDevice()
	if err != nil {
		log.Fatalf("Failed to get device: %v", err)
	}

	// Create client
	client = whatsmeow.NewClient(deviceStore, nil)
	if client.Store.ID == nil {
		qrChannel, _ := client.GetQRChannel(context.Background())
		err = client.Connect()
		if err != nil {
			log.Fatalf("Failed to connect: %v", err)
		}

		for evt := range qrChannel {
			if evt.Event == "code" {
				qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
			} else {
				log.Printf("QR Channel result: %s", evt.Event)
			}
		}
	} else {
		err = client.Connect()
		if err != nil {
			log.Fatalf("Failed to connect: %v", err)
		}
	}

	// Handle received messages and other events
	client.AddEventHandler(func(evt interface{}) {
		switch v := evt.(type) {
		case *events.Message:
			handleReceivedMessage(v)
		case *events.Connected:
			fmt.Println("Connected to WhatsApp")
		case *events.OfflineSyncCompleted:
			fmt.Println("Offline sync completed")
		case *events.LoggedOut:
			fmt.Println("Logged out")
		case *events.Disconnected:
			fmt.Println("Disconnected")
		default:
			fmt.Printf("Unhandled event: %T\n", v)
		}
	})

	// Set up Gin
	router := gin.Default()
	router.POST("/send", sendMessageHandler)
	log.Println("Starting server on port 8080")
	router.Run(":8080")
}

// Function to handle received messages
func handleReceivedMessage(message *events.Message) {
	sender := message.Info.Sender.String()
	msg := message.Message

	if msg.GetConversation() != "" {
		fmt.Printf("Received message from %s: %s\n", sender, msg.GetConversation())
	} else if msg.GetExtendedTextMessage() != nil {
		fmt.Printf("Received extended text message from %s: %s\n", sender, msg.GetExtendedTextMessage().GetText())
	} else {
		fmt.Printf("Received a message from %s, but could not determine its type\n", sender)
	}
}

// Function to send a message
func sendMessage(client *whatsmeow.Client, jid string, text string) error {
	targetJID := types.NewJID(jid, "s.whatsapp.net")
	msgID := client.GenerateMessageID()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second) // Increased timeout to 60 seconds
	defer cancel()

	_, err := client.SendMessage(ctx, targetJID, &waProto.Message{
		Conversation: proto.String(text),
	})
	if err != nil {
		log.Printf("Failed to send message: %v", err)
		return err
	}
	fmt.Println("Message sent, ID:", msgID)
	return nil
}

// Handler to send a message
func sendMessageHandler(c *gin.Context) {
	var request struct {
		JID  string `json:"jid" binding:"required"`
		Text string `json:"text" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		log.Println("Failed to bind JSON:", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	log.Println("Received request to send message:", request)
	err := sendMessage(client, request.JID, request.Text)
	if err != nil {
		log.Println("Failed to send message:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Println("Message sent successfully")
	c.JSON(http.StatusOK, gin.H{"status": "Message sent"})
}
