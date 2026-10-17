package detect

// Class is how formula v1 groups the tracker data's categories.
type Class string

const (
	ClassAdvertising Class = "advertising"
	ClassAnalytics   Class = "analytics"
	ClassAudience    Class = "audience measurement"
	ClassSocial      Class = "social"
	ClassReplay      Class = "session replay"
	ClassBlocklisted Class = "blocklisted request"
)

var classOf = map[string]Class{
	"Advertising":                     ClassAdvertising,
	"Ad Motivated Tracking":           ClassAdvertising,
	"Action Pixels":                   ClassAdvertising,
	"Analytics":                       ClassAnalytics,
	"Third-Party Analytics Marketing": ClassAnalytics,
	"Audience Measurement":            ClassAudience,
	"Social Network":                  ClassSocial,
	"Social - Share":                  ClassSocial,
	"Social - Comment":                ClassSocial,
}

// ClassOf returns the tracking class of a category, or false when the score
// does not count it (CDN, embedded content, tag managers, single sign-on).
func ClassOf(category string) (Class, bool) {
	c, ok := classOf[category]
	return c, ok
}
