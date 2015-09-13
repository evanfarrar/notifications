package acceptance

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"bitbucket.org/chrj/smtpd"

	"github.com/cloudfoundry-incubator/notifications/v2/acceptance/support"
	"github.com/pivotal-cf/uaa-sso-golang/uaa"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Campaign Lifecycle", func() {
	var (
		client   *support.Client
		token    uaa.Token
		senderID string
	)

	BeforeEach(func() {
		client = support.NewClient(support.Config{
			Host:  Servers.Notifications.URL(),
			Trace: Trace,
		})
		token = GetClientTokenFor("my-client")

		status, response, err := client.Do("POST", "/senders", map[string]interface{}{
			"name": "my-sender",
		}, token.Access)
		Expect(err).NotTo(HaveOccurred())
		Expect(status).To(Equal(http.StatusCreated))

		senderID = response["id"].(string)
	})

	Context("retrieving a campaign", func() {
		It("sends a campaign to an email and retrieves the campaign details", func() {
			var campaignTypeID, templateID, campaignID string

			By("creating a template", func() {
				status, response, err := client.Do("POST", "/templates", map[string]interface{}{
					"name":    "Acceptance Template",
					"text":    "campaign template {{.Text}}",
					"html":    "{{.HTML}}",
					"subject": "{{.Subject}}",
				}, token.Access)
				Expect(err).NotTo(HaveOccurred())
				Expect(status).To(Equal(http.StatusCreated))

				templateID = response["id"].(string)
			})

			By("creating a campaign type", func() {
				status, response, err := client.Do("POST", fmt.Sprintf("/senders/%s/campaign_types", senderID), map[string]interface{}{
					"name":        "some-campaign-type-name",
					"description": "acceptance campaign type",
				}, token.Access)
				Expect(err).NotTo(HaveOccurred())
				Expect(status).To(Equal(http.StatusCreated))

				campaignTypeID = response["id"].(string)
			})

			By("sending the campaign", func() {
				status, response, err := client.Do("POST", fmt.Sprintf("/senders/%s/campaigns", senderID), map[string]interface{}{
					"send_to": map[string]interface{}{
						"email": "test@example.com",
					},
					"campaign_type_id": campaignTypeID,
					"text":             "campaign body",
					"subject":          "campaign subject",
					"template_id":      templateID,
				}, token.Access)
				Expect(err).NotTo(HaveOccurred())
				Expect(status).To(Equal(http.StatusAccepted))
				Expect(response["campaign_id"]).NotTo(BeEmpty())

				campaignID = response["campaign_id"].(string)
			})

			By("retrieving the campaign details", func() {
				status, response, err := client.Do("GET", fmt.Sprintf("/senders/%s/campaigns/%s", senderID, campaignID), nil, token.Access)
				Expect(err).NotTo(HaveOccurred())
				Expect(status).To(Equal(http.StatusOK))
				Expect(response["id"]).To(Equal(campaignID))
				Expect(response["send_to"]).To(HaveKeyWithValue("email", "test@example.com"))
				Expect(response["campaign_type_id"]).To(Equal(campaignTypeID))
				Expect(response["text"]).To(Equal("campaign body"))
				Expect(response["subject"]).To(Equal("campaign subject"))
				Expect(response["template_id"]).To(Equal(templateID))
			})
		})

		Context("failure cases", func() {
			var campaignTypeID, templateID, campaignID string

			BeforeEach(func() {
				By("creating a template", func() {
					status, response, err := client.Do("POST", "/templates", map[string]interface{}{
						"name":    "Acceptance Template",
						"text":    "campaign template {{.Text}}",
						"html":    "{{.HTML}}",
						"subject": "{{.Subject}}",
					}, token.Access)
					Expect(err).NotTo(HaveOccurred())
					Expect(status).To(Equal(http.StatusCreated))

					templateID = response["id"].(string)
				})

				By("creating a campaign type", func() {
					status, response, err := client.Do("POST", fmt.Sprintf("/senders/%s/campaign_types", senderID), map[string]interface{}{
						"name":        "some-campaign-type-name",
						"description": "acceptance campaign type",
					}, token.Access)
					Expect(err).NotTo(HaveOccurred())
					Expect(status).To(Equal(http.StatusCreated))

					campaignTypeID = response["id"].(string)
				})

				By("sending a campaign", func() {
					status, response, err := client.Do("POST", fmt.Sprintf("/senders/%s/campaigns", senderID), map[string]interface{}{
						"send_to": map[string]interface{}{
							"email": "test@example.com",
						},
						"campaign_type_id": campaignTypeID,
						"text":             "campaign body",
						"subject":          "campaign subject",
						"template_id":      templateID,
					}, token.Access)
					Expect(err).NotTo(HaveOccurred())
					Expect(status).To(Equal(http.StatusAccepted))
					Expect(response["campaign_id"]).NotTo(BeEmpty())

					campaignID = response["campaign_id"].(string)
				})
			})

			It("verifies that the sender exists", func() {
				By("deleting the sender", func() {
					status, _, err := client.Do("DELETE", fmt.Sprintf("/senders/%s", senderID), map[string]interface{}{
						"name": "my-sender",
					}, token.Access)
					Expect(err).NotTo(HaveOccurred())
					Expect(status).To(Equal(http.StatusNoContent))
				})

				By("attempting to retrieve the campaign details", func() {
					status, response, err := client.Do("GET", fmt.Sprintf("/senders/%s/campaigns/%s", senderID, campaignID), nil, token.Access)
					Expect(err).NotTo(HaveOccurred())
					Expect(status).To(Equal(http.StatusNotFound))
					Expect(response["errors"]).To(ContainElement(fmt.Sprintf("Sender with id %q could not be found", senderID)))
				})
			})

			It("verifies that the sender belongs to the authenticated client", func() {
				token = GetClientTokenFor("other-client")

				By("attempting to retrieve the campaign details with a different token", func() {
					status, response, err := client.Do("GET", fmt.Sprintf("/senders/%s/campaigns/%s", senderID, campaignID), nil, token.Access)
					Expect(err).NotTo(HaveOccurred())
					Expect(status).To(Equal(http.StatusNotFound))
					Expect(response["errors"]).To(ContainElement(fmt.Sprintf("Sender with id %q could not be found", senderID)))
				})
			})

			It("verifies that the campaign exists", func() {
				By("attempting to retrieve an unknown campaign", func() {
					status, response, err := client.Do("GET", fmt.Sprintf("/senders/%s/campaigns/unknown-campaign-id", senderID), nil, token.Access)
					Expect(err).NotTo(HaveOccurred())
					Expect(status).To(Equal(http.StatusNotFound))
					Expect(response["errors"]).To(ContainElement("Campaign with id \"unknown-campaign-id\" could not be found"))
				})
			})

			It("verifies that the campaign belongs to the sender", func() {
				var otherSenderID string

				By("creating another sender", func() {
					status, response, err := client.Do("POST", "/senders", map[string]interface{}{
						"name": "another-sender",
					}, token.Access)
					Expect(err).NotTo(HaveOccurred())
					Expect(status).To(Equal(http.StatusCreated))

					otherSenderID = response["id"].(string)
				})

				By("attempting to retrieve a campaign with a different sender", func() {
					status, response, err := client.Do("GET", fmt.Sprintf("/senders/%s/campaigns/%s", otherSenderID, campaignID), nil, token.Access)
					Expect(err).NotTo(HaveOccurred())
					Expect(status).To(Equal(http.StatusNotFound))
					Expect(response["errors"]).To(ContainElement(fmt.Sprintf("Campaign with id %q could not be found", campaignID)))
				})
			})

			It("verifies that the notifications.write scope is required", func() {
				token = GetClientTokenFor("unauthorized-client")

				By("attempting to retrieve the campaign details with a different token", func() {
					status, response, err := client.Do("GET", fmt.Sprintf("/senders/%s/campaigns/%s", senderID, campaignID), nil, token.Access)
					Expect(err).NotTo(HaveOccurred())
					Expect(status).To(Equal(http.StatusForbidden))
					Expect(response["errors"]).To(ContainElement("You are not authorized to perform the requested action"))
				})
			})
		})
	})

	Context("retrieving a campaign status", func() {
		var campaignTypeID, templateID, campaignID string

		BeforeEach(func() {
			By("creating a template", func() {
				status, response, err := client.Do("POST", "/templates", map[string]interface{}{
					"name":    "Acceptance Template",
					"text":    "campaign template {{.Text}}",
					"html":    "{{.HTML}}",
					"subject": "{{.Subject}}",
				}, token.Access)
				Expect(err).NotTo(HaveOccurred())
				Expect(status).To(Equal(http.StatusCreated))

				templateID = response["id"].(string)
			})

			By("creating a campaign type", func() {
				status, response, err := client.Do("POST", fmt.Sprintf("/senders/%s/campaign_types", senderID), map[string]interface{}{
					"name":        "some-campaign-type-name",
					"description": "acceptance campaign type",
				}, token.Access)
				Expect(err).NotTo(HaveOccurred())
				Expect(status).To(Equal(http.StatusCreated))

				campaignTypeID = response["id"].(string)
			})
		})

		It("sends a campaign to an email and retrieves the campaign status once the campaign is complete", func() {
			By("sending the campaign", func() {
				status, response, err := client.Do("POST", fmt.Sprintf("/senders/%s/campaigns", senderID), map[string]interface{}{
					"send_to": map[string]interface{}{
						"email": "test@example.com",
					},
					"campaign_type_id": campaignTypeID,
					"text":             "campaign body",
					"subject":          "campaign subject",
					"template_id":      templateID,
				}, token.Access)
				Expect(err).NotTo(HaveOccurred())
				Expect(status).To(Equal(http.StatusAccepted))
				Expect(response["campaign_id"]).NotTo(BeEmpty())

				campaignID = response["campaign_id"].(string)
			})

			By("waiting for the email to arrive", func() {
				Eventually(func() []smtpd.Envelope {
					return Servers.SMTP.Deliveries
				}, "10s").Should(HaveLen(1))
			})

			By("retrieving the campaign status", func() {
				status, response, err := client.Do("GET", fmt.Sprintf("/senders/%s/campaigns/%s/status", senderID, campaignID), nil, token.Access)
				Expect(err).NotTo(HaveOccurred())
				Expect(status).To(Equal(http.StatusOK))
				Expect(response["id"]).To(Equal(campaignID))
				Expect(response["status"]).To(Equal("completed"))
				Expect(response["total_messages"]).To(Equal(float64(1)))
				Expect(response["sent_messages"]).To(Equal(float64(1)))
				Expect(response["retry_messages"]).To(Equal(float64(0)))
				Expect(response["failed_messages"]).To(Equal(float64(0)))

				startTime, err := time.Parse(time.RFC3339, response["start_time"].(string))
				Expect(err).NotTo(HaveOccurred())

				completedTime, err := time.Parse(time.RFC3339, response["completed_time"].(string))
				Expect(err).NotTo(HaveOccurred())

				Expect(startTime).To(BeTemporally("~", time.Now(), 10*time.Second))
				Expect(completedTime).To(BeTemporally("~", time.Now(), 10*time.Second))
			})
		})

		Context("when the SMTP server is erroring", func() {
			BeforeEach(func() {
				Servers.SMTP.HandlerCall.Returns.Error = errors.New("some error")
			})

			AfterEach(func() {
				Servers.Notifications.ResetDatabase()
			})

			It("sends a campaign to an email and retrieves the campaign status before the campaign completes", func() {
				By("sending the campaign", func() {
					status, response, err := client.Do("POST", fmt.Sprintf("/senders/%s/campaigns", senderID), map[string]interface{}{
						"send_to": map[string]interface{}{
							"email": "test@example.com",
						},
						"campaign_type_id": campaignTypeID,
						"text":             "campaign body",
						"subject":          "campaign subject",
						"template_id":      templateID,
					}, token.Access)
					Expect(err).NotTo(HaveOccurred())
					Expect(status).To(Equal(http.StatusAccepted))
					Expect(response["campaign_id"]).NotTo(BeEmpty())

					campaignID = response["campaign_id"].(string)
				})

				By("waiting for the status to indicate retrying", func() {
					Eventually(func() (interface{}, error) {
						_, response, err := client.Do("GET", fmt.Sprintf("/senders/%s/campaigns/%s/status", senderID, campaignID), nil, token.Access)
						return response["retry_messages"], err
					}, "10s").Should(Equal(float64(1)))
				})

				By("retrieving the campaign status", func() {
					status, response, err := client.Do("GET", fmt.Sprintf("/senders/%s/campaigns/%s/status", senderID, campaignID), nil, token.Access)
					Expect(err).NotTo(HaveOccurred())
					Expect(status).To(Equal(http.StatusOK))

					Expect(response["id"]).To(Equal(campaignID))
					Expect(response["status"]).To(Equal("sending"))
					Expect(response["total_messages"]).To(Equal(float64(1)))
					Expect(response["sent_messages"]).To(Equal(float64(0)))
					Expect(response["retry_messages"]).To(Equal(float64(1)))
					Expect(response["failed_messages"]).To(Equal(float64(0)))

					startTime, err := time.Parse(time.RFC3339, response["start_time"].(string))
					Expect(err).NotTo(HaveOccurred())
					Expect(startTime).To(BeTemporally("~", time.Now(), 10*time.Second))

					Expect(response["completed_time"]).To(BeNil())
				})
			})
		})

		Context("when there are jobs queueing up", func() {
			BeforeEach(func() {
				Servers.SMTP.HandlerCall.Callback = func() {
					time.Sleep(500 * time.Millisecond)
				}
			})

			It("sends a campaign to an email and retrieves the campaign status before the campaign completes", func() {
				By("sending the campaign", func() {
					status, response, err := client.Do("POST", fmt.Sprintf("/senders/%s/campaigns", senderID), map[string]interface{}{
						"send_to": map[string]interface{}{
							"space": "large-space",
						},
						"campaign_type_id": campaignTypeID,
						"text":             "campaign body",
						"subject":          "campaign subject",
						"template_id":      templateID,
					}, token.Access)
					Expect(err).NotTo(HaveOccurred())
					Expect(status).To(Equal(http.StatusAccepted))
					Expect(response["campaign_id"]).NotTo(BeEmpty())

					campaignID = response["campaign_id"].(string)
				})

				By("waiting for the status to indicate queueing", func() {
					Eventually(func() (interface{}, error) {
						_, response, err := client.Do("GET", fmt.Sprintf("/senders/%s/campaigns/%s/status", senderID, campaignID), nil, token.Access)
						return response["queued_messages"], err
					}, "10s").Should(BeNumerically(">", 0))
				})
			})
		})
	})
})