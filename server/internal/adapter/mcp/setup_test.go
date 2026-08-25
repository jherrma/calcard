package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jherrma/caldav-server/internal/adapter/repository"
	"github.com/jherrma/caldav-server/internal/domain/addressbook"
	"github.com/jherrma/caldav-server/internal/domain/calendar"
	"github.com/jherrma/caldav-server/internal/domain/sharing"
	domainuser "github.com/jherrma/caldav-server/internal/domain/user"
	"github.com/jherrma/caldav-server/internal/infrastructure/logging"
	addressbookuc "github.com/jherrma/caldav-server/internal/usecase/addressbook"
	calendaruc "github.com/jherrma/caldav-server/internal/usecase/calendar"
	contactuc "github.com/jherrma/caldav-server/internal/usecase/contact"
	eventuc "github.com/jherrma/caldav-server/internal/usecase/event"
	searchuc "github.com/jherrma/caldav-server/internal/usecase/search"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// fixedNow pins the clock so scheduling assertions are stable. It is a
// Wednesday, which keeps weekday-sensitive recurrence fixtures readable.
var fixedNow = time.Date(2026, 3, 4, 9, 0, 0, 0, time.UTC)

// testEnv is a fully wired MCP server over an in-memory database.
//
// It deliberately builds the REAL use cases and repositories rather than
// mocking them: the property these tests exist to protect is that MCP reaches
// the same data, with the same permissions, as REST — which a mock would assume
// rather than verify.
type testEnv struct {
	t          *testing.T
	server     *Server
	db         *gorm.DB
	calRepo    calendar.CalendarRepository
	abRepo     addressbook.Repository
	userRepo   domainuser.UserRepository
	shareRepo  sharing.CalendarShareRepository
	abShare    sharing.AddressBookShareRepository
	mcpTokens  domainuser.MCPTokenRepository
	securityLg *logging.SecurityLogger
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&domainuser.User{},
		&domainuser.MCPToken{},
		&calendar.Calendar{},
		&calendar.CalendarObject{},
		&calendar.SyncChangeLog{},
		&addressbook.AddressBook{},
		&addressbook.AddressObject{},
		&addressbook.ContactPhoto{},
		&addressbook.SyncChangeLog{},
		&sharing.CalendarShare{},
		&sharing.AddressBookShare{},
	))

	calRepo := repository.NewCalendarRepository(db)
	abRepo := repository.NewAddressBookRepository(db)
	userRepo := repository.NewUserRepository(db)
	shareRepo := repository.NewCalendarShareRepository(db)
	abShareRepo := repository.NewAddressBookShareRepository(db)
	mcpTokenRepo := repository.NewMCPTokenRepository(db)

	calList := calendaruc.NewListCalendarsUseCase(calRepo, shareRepo)
	abList := addressbookuc.NewListUseCase(abRepo, abShareRepo)

	deps := Deps{
		CalendarRepo:    calRepo,
		AddressBookRepo: abRepo,
		CalendarList:    calList,
		EventList:       eventuc.NewListEventsUseCase(calRepo),
		EventGet:        eventuc.NewGetEventUseCase(calRepo),
		EventCreate:     eventuc.NewCreateEventUseCase(calRepo),
		EventUpdate:     eventuc.NewUpdateEventUseCase(calRepo),
		EventDelete:     eventuc.NewDeleteEventUseCase(calRepo),
		AddressBookList: abList,
		ContactList:     contactuc.NewListUseCase(abRepo),
		ContactGet:      contactuc.NewGetUseCase(abRepo),
		ContactCreate:   contactuc.NewCreateUseCase(addressbookuc.NewCreateContactUseCase(abRepo)),
		ContactUpdate:   contactuc.NewUpdateUseCase(abRepo),
		ContactDelete:   contactuc.NewDeleteUseCase(abRepo),
		Search:          searchuc.NewUseCase(calRepo, abRepo, calList, abList),
	}

	srv := NewServer(deps)
	srv.now = func() time.Time { return fixedNow }

	return &testEnv{
		t:          t,
		server:     srv,
		db:         db,
		calRepo:    calRepo,
		abRepo:     abRepo,
		userRepo:   userRepo,
		shareRepo:  shareRepo,
		abShare:    abShareRepo,
		mcpTokens:  mcpTokenRepo,
		securityLg: logging.NewSecurityLogger(slog.New(slog.NewJSONHandler(io.Discard, nil))),
	}
}

func (e *testEnv) ctx() context.Context { return context.Background() }

// newUser inserts an active user and returns it.
func (e *testEnv) newUser(email string) *domainuser.User {
	e.t.Helper()
	u := &domainuser.User{
		UUID:         uuid.New().String(),
		Email:        email,
		Username:     email,
		PasswordHash: "x",
		DisplayName:  email,
		IsActive:     true,
	}
	require.NoError(e.t, e.userRepo.Create(e.ctx(), u))
	return u
}

// newCalendar inserts a calendar owned by userID.
func (e *testEnv) newCalendar(userID uint, name string) *calendar.Calendar {
	e.t.Helper()
	cal := &calendar.Calendar{
		UUID:                uuid.New().String(),
		UserID:              userID,
		Path:                name,
		Name:                name,
		Color:               "#3b82f6",
		Timezone:            "UTC",
		SupportedComponents: "VEVENT",
	}
	require.NoError(e.t, e.calRepo.Create(e.ctx(), cal))
	return cal
}

// newAddressBook inserts an address book owned by userID.
func (e *testEnv) newAddressBook(userID uint, name string) *addressbook.AddressBook {
	e.t.Helper()
	ab := &addressbook.AddressBook{
		UUID:   uuid.New().String(),
		UserID: userID,
		Path:   name,
		Name:   name,
	}
	require.NoError(e.t, e.abRepo.Create(e.ctx(), ab))
	return ab
}

// shareCalendar grants granteeID the given permission on cal.
func (e *testEnv) shareCalendar(cal *calendar.Calendar, granteeID uint, permission string) {
	e.t.Helper()
	require.NoError(e.t, e.shareRepo.Create(e.ctx(), &sharing.CalendarShare{
		UUID:         uuid.New().String(),
		CalendarID:   cal.ID,
		SharedWithID: granteeID,
		Permission:   permission,
	}))
}

// shareAddressBook grants granteeID the given permission on ab.
func (e *testEnv) shareAddressBook(ab *addressbook.AddressBook, granteeID uint, permission string) {
	e.t.Helper()
	require.NoError(e.t, e.abShare.Create(e.ctx(), &sharing.AddressBookShare{
		UUID:          uuid.New().String(),
		AddressBookID: ab.ID,
		SharedWithID:  granteeID,
		Permission:    permission,
	}))
}

// call invokes one tool and returns its decoded JSON payload. It fails the test
// on a protocol error, since a test that meant to exercise a tool has no use
// for "the call never ran".
func (e *testEnv) call(userID uint, tool string, args map[string]interface{}) (map[string]interface{}, bool) {
	e.t.Helper()
	result := e.callRaw(userID, tool, args)
	if result.IsError {
		return map[string]interface{}{"error": result.Content[0].Text}, true
	}
	var payload map[string]interface{}
	require.NoError(e.t, json.Unmarshal([]byte(result.Content[0].Text), &payload),
		"tool %s returned non-JSON content: %s", tool, result.Content[0].Text)
	return payload, false
}

// callRaw invokes one tool and returns the MCP result, error or not.
func (e *testEnv) callRaw(userID uint, tool string, args map[string]interface{}) *toolCallResult {
	e.t.Helper()
	encoded, err := json.Marshal(args)
	require.NoError(e.t, err)

	fn, ok := e.server.tools[tool]
	require.True(e.t, ok, "no such tool %q", tool)

	cc := &callContext{ctx: e.ctx(), userID: userID, now: fixedNow}
	result, rpcErr := fn(cc, encoded)
	require.Nil(e.t, rpcErr, "tool %s failed at the protocol level: %v", tool, rpcErr)
	require.NotNil(e.t, result)
	require.NotEmpty(e.t, result.Content)
	return result
}

// rpc drives the dispatcher the way the transport does.
func (e *testEnv) rpc(userID uint, method string, params interface{}) *Response {
	e.t.Helper()
	var raw json.RawMessage
	if params != nil {
		encoded, err := json.Marshal(params)
		require.NoError(e.t, err)
		raw = encoded
	}
	resp, _ := e.server.Handle(e.ctx(), userID, &Request{
		JSONRPC: jsonRPCVersion,
		ID:      json.RawMessage(`1`),
		Method:  method,
		Params:  raw,
	})
	return resp
}
