package acceptance

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/cloudfoundry-incubator/notifications/acceptance/servers"
	"github.com/cloudfoundry-incubator/notifications/acceptance/support"
	"github.com/cloudfoundry-incubator/notifications/application"
	"github.com/cloudfoundry-incubator/notifications/models"
	"github.com/cloudfoundry-incubator/notifications/web/services"
	"github.com/pivotal-cf/uaa-sso-golang/uaa"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Preferences Endpoint", func() {
	It("client unsubscribes a user from a notification", func() {
		// Retrieve Client UAA token
		env := application.NewEnvironment()
		uaaClient := uaa.NewUAA("", env.UAAHost, "notifications-sender", "secret", "")
		clientToken, err := uaaClient.GetClientToken()
		if err != nil {
			panic(err)
		}

		userGUID := "user-123"

		test := ManageArbitraryUsersPreferences{
			client:              support.NewClient(Servers.Notifications),
			notificationsServer: Servers.Notifications,
			clientToken:         clientToken,
			smtpServer:          Servers.SMTP,
			userGUID:            userGUID,
		}

		test.RegisterClientNotifications()
		test.SendNotificationToUser()
		test.RetrieveUserPreferences()

		// Notification Unsubscribe
		test.UnsubscribeFromNotification()
		test.ConfirmUserUnsubscribed()
		test.ConfirmUserDoesNotReceiveNotification()

		// Global Unsubscribe
		test.GlobalUnsubscribe()
		test.ConfirmGlobalUnsubscribe()
		test.ConfirmUserDoesNotReceiveNotificationsGlobal()
		test.UndoGlobalUnsubscribe()
		test.ReConfirmUserUnsubscribed()
		test.ConfirmUserReceivesNotificationsGlobal()
	})
})

type ManageArbitraryUsersPreferences struct {
	client              *support.Client
	notificationsServer servers.Notifications
	smtpServer          *servers.SMTP
	clientToken         uaa.Token
	userGUID            string
}

// Make request to /registation
func (t ManageArbitraryUsersPreferences) RegisterClientNotifications() {
	code, err := t.client.Notifications.Register(t.clientToken.Access, support.RegisterClient{
		SourceName: "Notifications Sender",
		Notifications: map[string]support.RegisterNotification{
			"acceptance-test": {
				Description: "Acceptance Test",
			},
			"unsubscribe-acceptance-test": {
				Description: "Unsubscribe Acceptance Test",
			},
		},
	})

	Expect(err).NotTo(HaveOccurred())
	Expect(code).To(Equal(http.StatusNoContent))
}

// Make request to /users/:guid
func (t ManageArbitraryUsersPreferences) SendNotificationToUser() {
	body, err := json.Marshal(map[string]string{
		"kind_id": "unsubscribe-acceptance-test",
		"html":    "<p>this is an acceptance test</p>",
		"subject": "my-special-subject",
	})
	if err != nil {
		panic(err)
	}

	request, err := http.NewRequest("POST", t.notificationsServer.UsersPath("user-123"), bytes.NewBuffer(body))
	if err != nil {
		panic(err)
	}

	request.Header.Set("Authorization", "Bearer "+t.clientToken.Access)

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		panic(err)
	}

	// Confirm the request response looks correct
	Expect(response.StatusCode).To(Equal(http.StatusOK))

	responseJSON := []map[string]string{}
	err = json.NewDecoder(response.Body).Decode(&responseJSON)
	if err != nil {
		panic(err)
	}

	Expect(len(responseJSON)).To(Equal(1))
	responseItem := responseJSON[0]
	Expect(responseItem["status"]).To(Equal("queued"))
	Expect(responseItem["recipient"]).To(Equal("user-123"))
	Expect(GUIDRegex.MatchString(responseItem["notification_id"])).To(BeTrue())

	// Confirm the email message was delivered correctly
	Eventually(func() int {
		return len(t.smtpServer.Deliveries)
	}, 5*time.Second).Should(Equal(1))
	delivery := t.smtpServer.Deliveries[0]

	env := application.NewEnvironment()
	Expect(delivery.Sender).To(Equal(env.Sender))
	Expect(delivery.Recipients).To(Equal([]string{"user-123@example.com"}))

	data := strings.Split(string(delivery.Data), "\n")
	Expect(data).To(ContainElement("X-CF-Client-ID: notifications-sender"))
	Expect(data).To(ContainElement("X-CF-Notification-ID: " + responseItem["notification_id"]))
	Expect(data).To(ContainElement("Subject: CF Notification: my-special-subject"))
	Expect(data).To(ContainElement(`<p>The following "Unsubscribe Acceptance Test" notification was sent to you directly by the`))
	Expect(data).To(ContainElement(`    "Notifications Sender" component of Cloud Foundry:</p>`))
	Expect(data).To(ContainElement("<p>this is an acceptance test</p>"))
}

func (t ManageArbitraryUsersPreferences) RetrieveUserPreferences() {
	request, err := http.NewRequest("GET", t.notificationsServer.SpecificUserPreferencesPath(t.userGUID), nil)
	if err != nil {
		panic(err)
	}

	request.Header.Set("Authorization", "Bearer "+t.clientToken.Access)

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		panic(err)
	}

	// Confirm the request response looks correct
	Expect(response.StatusCode).To(Equal(http.StatusOK))

	prefsResponseJSON := services.PreferencesBuilder{}
	err = json.NewDecoder(response.Body).Decode(&prefsResponseJSON)
	if err != nil {
		panic(err)
	}

	node := prefsResponseJSON.Clients["notifications-sender"]["acceptance-test"]
	Expect(node.Email).To(Equal(&TRUE))
	Expect(node.KindDescription).To(Equal("Acceptance Test"))
	Expect(node.SourceDescription).To(Equal("Notifications Sender"))
	Expect(node.Count).To(Equal(0))

	node = prefsResponseJSON.Clients["notifications-sender"]["unsubscribe-acceptance-test"]
	Expect(node.Email).To(Equal(&TRUE))
	Expect(node.KindDescription).To(Equal("Unsubscribe Acceptance Test"))
	Expect(node.SourceDescription).To(Equal("Notifications Sender"))
	Expect(node.Count).To(Equal(1))
}

// Make a PATCH request to /user_preferences/:userGUID
func (t ManageArbitraryUsersPreferences) UnsubscribeFromNotification() {
	builder := services.NewPreferencesBuilder()
	builder.Add(models.Preference{
		ClientID: "notifications-sender",
		KindID:   "unsubscribe-acceptance-test",
		Email:    false,
		Count:    123,
	})

	body, err := json.Marshal(builder)
	if err != nil {
		panic(err)
	}

	request, err := http.NewRequest("PATCH", t.notificationsServer.SpecificUserPreferencesPath(t.userGUID), bytes.NewBuffer(body))
	if err != nil {
		panic(err)
	}

	request.Header.Set("Authorization", "Bearer "+t.clientToken.Access)

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		panic(err)
	}

	// Confirm the request response looks correct
	Expect(response.StatusCode).To(Equal(http.StatusNoContent))
}

func (t ManageArbitraryUsersPreferences) ConfirmUserUnsubscribed() {
	request, err := http.NewRequest("GET", t.notificationsServer.SpecificUserPreferencesPath(t.userGUID), nil)
	if err != nil {
		panic(err)
	}

	request.Header.Set("Authorization", "Bearer "+t.clientToken.Access)

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		panic(err)
	}
	Expect(response.StatusCode).To(Equal(http.StatusOK))

	prefsResponseJSON := services.PreferencesBuilder{}
	err = json.NewDecoder(response.Body).Decode(&prefsResponseJSON)
	if err != nil {
		panic(err)
	}

	node := prefsResponseJSON.Clients["notifications-sender"]["acceptance-test"]
	Expect(node.Email).To(Equal(&TRUE))
	Expect(node.KindDescription).To(Equal("Acceptance Test"))
	Expect(node.SourceDescription).To(Equal("Notifications Sender"))
	Expect(node.Count).To(Equal(0))

	node = prefsResponseJSON.Clients["notifications-sender"]["unsubscribe-acceptance-test"]
	Expect(node.Email).To(Equal(&FALSE))
	Expect(node.KindDescription).To(Equal("Unsubscribe Acceptance Test"))
	Expect(node.SourceDescription).To(Equal("Notifications Sender"))
	Expect(node.Count).To(Equal(1))
}

func (t ManageArbitraryUsersPreferences) ConfirmUserDoesNotReceiveNotification() {
	t.smtpServer.Reset()

	body, err := json.Marshal(map[string]string{
		"kind_id": "unsubscribe-acceptance-test",
		"html":    "<p>this is an acceptance test</p>",
		"subject": "my-special-subject",
	})
	if err != nil {
		panic(err)
	}

	request, err := http.NewRequest("POST", t.notificationsServer.UsersPath("user-123"), bytes.NewBuffer(body))
	if err != nil {
		panic(err)
	}

	request.Header.Set("Authorization", "Bearer "+t.clientToken.Access)

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		panic(err)
	}

	// Confirm the request response looks correct
	Expect(response.StatusCode).To(Equal(http.StatusOK))

	responseJSON := []map[string]string{}
	err = json.NewDecoder(response.Body).Decode(&responseJSON)
	if err != nil {
		panic(err)
	}

	Expect(len(responseJSON)).To(Equal(1))
	responseItem := responseJSON[0]
	Expect(responseItem["status"]).To(Equal("queued"))
	Expect(responseItem["recipient"]).To(Equal("user-123"))
	Expect(GUIDRegex.MatchString(responseItem["notification_id"])).To(BeTrue())

	// Confirm the email message never gets delivered
	Consistently(func() int {
		return len(t.smtpServer.Deliveries)
	}, 5*time.Second).Should(Equal(0))
}

func (t ManageArbitraryUsersPreferences) GlobalUnsubscribe() {
	requestBodyPayload := map[string]interface{}{
		"global_unsubscribe": true,
		"clients":            map[string]interface{}{},
	}

	body, err := json.Marshal(requestBodyPayload)
	if err != nil {
		panic(err)
	}

	request, err := http.NewRequest("PATCH", t.notificationsServer.SpecificUserPreferencesPath(t.userGUID), bytes.NewBuffer(body))
	if err != nil {
		panic(err)
	}

	request.Header.Set("Authorization", "Bearer "+t.clientToken.Access)

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		panic(err)
	}

	// Confirm the request response looks correct
	Expect(response.StatusCode).To(Equal(http.StatusNoContent))
}

func (t ManageArbitraryUsersPreferences) ConfirmGlobalUnsubscribe() {
	request, err := http.NewRequest("GET", t.notificationsServer.SpecificUserPreferencesPath(t.userGUID), nil)
	if err != nil {
		panic(err)
	}

	request.Header.Set("Authorization", "Bearer "+t.clientToken.Access)

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		panic(err)
	}

	// Confirm the request response looks correct
	Expect(response.StatusCode).To(Equal(http.StatusOK))

	prefsResponseJSON := services.PreferencesBuilder{}
	err = json.NewDecoder(response.Body).Decode(&prefsResponseJSON)
	if err != nil {
		panic(err)
	}

	Expect(prefsResponseJSON.GlobalUnsubscribe).To(BeTrue())
}

func (t ManageArbitraryUsersPreferences) ConfirmUserDoesNotReceiveNotificationsGlobal() {
	t.smtpServer.Reset()

	body, err := json.Marshal(map[string]string{
		"kind_id": "acceptance-test",
		"html":    "<p>this is an acceptance test</p>",
		"subject": "my-special-subject",
	})
	if err != nil {
		panic(err)
	}

	request, err := http.NewRequest("POST", t.notificationsServer.UsersPath("user-123"), bytes.NewBuffer(body))
	if err != nil {
		panic(err)
	}

	request.Header.Set("Authorization", "Bearer "+t.clientToken.Access)

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		panic(err)
	}

	// Confirm the request response looks correct
	Expect(response.StatusCode).To(Equal(http.StatusOK))

	responseJSON := []map[string]string{}
	err = json.NewDecoder(response.Body).Decode(&responseJSON)
	if err != nil {
		panic(err)
	}

	Expect(len(responseJSON)).To(Equal(1))
	responseItem := responseJSON[0]
	Expect(responseItem["status"]).To(Equal("queued"))
	Expect(responseItem["recipient"]).To(Equal("user-123"))
	Expect(GUIDRegex.MatchString(responseItem["notification_id"])).To(BeTrue())

	// Confirm the email message never gets delivered
	Consistently(func() int {
		return len(t.smtpServer.Deliveries)
	}, 5*time.Second).Should(Equal(0))
}

func (t ManageArbitraryUsersPreferences) UndoGlobalUnsubscribe() {
	requestBodyPayload := map[string]interface{}{
		"global_unsubscribe": false,
		"clients":            map[string]interface{}{},
	}

	body, err := json.Marshal(requestBodyPayload)
	if err != nil {
		panic(err)
	}

	request, err := http.NewRequest("PATCH", t.notificationsServer.SpecificUserPreferencesPath(t.userGUID), bytes.NewBuffer(body))
	if err != nil {
		panic(err)
	}

	request.Header.Set("Authorization", "Bearer "+t.clientToken.Access)

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		panic(err)
	}
	// Confirm the request response looks correct
	Expect(response.StatusCode).To(Equal(http.StatusNoContent))
}

func (t ManageArbitraryUsersPreferences) ReConfirmUserUnsubscribed() {
	request, err := http.NewRequest("GET", t.notificationsServer.SpecificUserPreferencesPath(t.userGUID), nil)
	if err != nil {
		panic(err)
	}

	request.Header.Set("Authorization", "Bearer "+t.clientToken.Access)

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		panic(err)
	}

	// Confirm the request response looks correct
	Expect(response.StatusCode).To(Equal(http.StatusOK))

	prefsResponseJSON := services.PreferencesBuilder{}
	err = json.NewDecoder(response.Body).Decode(&prefsResponseJSON)
	if err != nil {
		panic(err)
	}

	Expect(prefsResponseJSON.GlobalUnsubscribe).To(BeFalse())
}

func (t ManageArbitraryUsersPreferences) ConfirmUserReceivesNotificationsGlobal() {
	t.smtpServer.Reset()

	body, err := json.Marshal(map[string]string{
		"kind_id": "acceptance-test",
		"html":    "<p>this is an acceptance test</p>",
		"subject": "my-special-subject",
	})
	if err != nil {
		panic(err)
	}

	request, err := http.NewRequest("POST", t.notificationsServer.UsersPath("user-123"), bytes.NewBuffer(body))
	if err != nil {
		panic(err)
	}

	request.Header.Set("Authorization", "Bearer "+t.clientToken.Access)

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		panic(err)
	}

	// Confirm the request response looks correct
	Expect(response.StatusCode).To(Equal(http.StatusOK))

	responseJSON := []map[string]string{}
	err = json.NewDecoder(response.Body).Decode(&responseJSON)
	if err != nil {
		panic(err)
	}

	Expect(len(responseJSON)).To(Equal(1))
	responseItem := responseJSON[0]
	Expect(responseItem["status"]).To(Equal("queued"))
	Expect(responseItem["recipient"]).To(Equal("user-123"))
	Expect(GUIDRegex.MatchString(responseItem["notification_id"])).To(BeTrue())

	// Confirm the email message gets delivered
	Eventually(func() int {
		return len(t.smtpServer.Deliveries)
	}, 5*time.Second).Should(Equal(1))
}
