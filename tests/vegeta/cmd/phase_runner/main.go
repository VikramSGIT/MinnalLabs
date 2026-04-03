package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
)

type createdHome struct {
	UserIndex int
	HomeSlot  int
	HomeID    int
	Name      string
	CreatedAt time.Time
}

type trackedHome struct {
	createdHome
	polls       int
	earlyChecked bool
	done        bool
}

type phaseProgress struct {
	submitted atomic.Int64
	completed atomic.Int64
}

type phasePacer struct {
	startRPS   float64
	peakRPS    float64
	rampUp     time.Duration
	hold       time.Duration
	rampDown   time.Duration
	totalStage time.Duration
}

var _ vegeta.Pacer = (*phasePacer)(nil)

func newPhasePacer(startRPS, peakRPS float64, rampUp, hold, rampDown time.Duration) *phasePacer {
	total := rampUp + hold + rampDown
	return &phasePacer{
		startRPS:   startRPS,
		peakRPS:    peakRPS,
		rampUp:     rampUp,
		hold:       hold,
		rampDown:   rampDown,
		totalStage: total,
	}
}

func (p *phasePacer) Pace(elapsed time.Duration, hits uint64) (time.Duration, bool) {
	targetHits := float64(hits + 1)
	if p.totalStage > 0 && p.hits(p.totalStage)+1e-9 < targetHits {
		return 0, true
	}
	if p.hits(elapsed) >= targetHits {
		return 0, false
	}

	low := elapsed
	high := elapsed
	if high <= 0 {
		high = time.Millisecond
	}
	for {
		if p.hits(high) >= targetHits {
			break
		}
		if p.totalStage > 0 && high >= p.totalStage {
			if p.hits(high)+1e-9 < targetHits {
				return 0, true
			}
			break
		}
		next := high * 2
		if next <= high {
			next = high + time.Millisecond
		}
		if p.totalStage > 0 && next > p.totalStage {
			next = p.totalStage
		}
		high = next
	}

	for i := 0; i < 24; i++ {
		mid := low + (high-low)/2
		if p.hits(mid) >= targetHits {
			high = mid
		} else {
			low = mid
		}
	}
	if high < elapsed {
		return 0, false
	}
	return high - elapsed, false
}

func (p *phasePacer) Rate(elapsed time.Duration) float64 {
	if p.totalStage == 0 {
		return p.peakRPS
	}
	if elapsed <= 0 {
		if p.rampUp > 0 {
			return p.startRPS
		}
		if p.hold > 0 {
			return p.peakRPS
		}
		return p.peakRPS
	}
	if p.rampUp > 0 && elapsed < p.rampUp {
		progress := elapsed.Seconds() / p.rampUp.Seconds()
		return p.startRPS + (p.peakRPS-p.startRPS)*progress
	}
	elapsed -= p.rampUp
	if p.hold > 0 && elapsed < p.hold {
		return p.peakRPS
	}
	elapsed -= p.hold
	if p.rampDown > 0 && elapsed < p.rampDown {
		progress := elapsed.Seconds() / p.rampDown.Seconds()
		return math.Max(0, p.peakRPS*(1-progress))
	}
	if p.totalStage > 0 && elapsed >= p.rampDown {
		return 0
	}
	return p.peakRPS
}

func (p *phasePacer) hits(elapsed time.Duration) float64 {
	if elapsed <= 0 {
		return 0
	}
	total := 0.0
	remaining := elapsed
	if p.rampUp > 0 {
		stage := minDuration(remaining, p.rampUp)
		x := stage.Seconds()
		a := (p.peakRPS - p.startRPS) / p.rampUp.Seconds()
		total += p.startRPS*x + 0.5*a*x*x
		remaining -= stage
		if remaining <= 0 {
			return total
		}
	}
	if p.hold > 0 {
		stage := minDuration(remaining, p.hold)
		total += p.peakRPS * stage.Seconds()
		remaining -= stage
		if remaining <= 0 {
			return total
		}
	}
	if p.rampDown > 0 {
		stage := minDuration(remaining, p.rampDown)
		x := stage.Seconds()
		a := p.peakRPS / p.rampDown.Seconds()
		total += p.peakRPS*x - 0.5*a*x*x
	}
	return total
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	state, err := loadPhaseStateStore(cfg.PhaseStatePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load phase state: %v\n", err)
		os.Exit(1)
	}

	metrics, err := newMetricCollector(cfg.RawPath, cfg.SummaryPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "init metrics: %v\n", err)
		os.Exit(1)
	}
	defer metrics.Close()

	stopFlush := make(chan struct{})
	state.StartAutoFlush(stopFlush, 500*time.Millisecond, func(format string, args ...any) {
		fmt.Printf(format+"\n", args...)
	})

	runner := newRunner(cfg, state, metrics)
	err = runner.run()
	close(stopFlush)
	flushErr := state.Flush()
	summaryErr := metrics.WriteSummary()
	metrics.PrintPhaseSummary(cfg.Phase.Name)

	if flushErr != nil {
		fmt.Fprintf(os.Stderr, "flush phase state: %v\n", flushErr)
	}
	if summaryErr != nil {
		fmt.Fprintf(os.Stderr, "write phase summary: %v\n", summaryErr)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	if flushErr != nil || summaryErr != nil {
		os.Exit(1)
	}
}

func (r *Runner) run() error {
	switch r.cfg.Phase.Name {
	case "create_users":
		return r.runCreateUsers()
	case "google_oauth_enroll":
		return r.runGoogleOAuthEnroll()
	case "google_signin_enroll":
		return r.runGoogleSignInEnroll()
	case "create_homes":
		return r.runCreateHomes()
	case "enroll_devices":
		return r.runEnrollDevices()
	case "fulfillment_mix":
		return r.runFulfillmentMix()
	case "delete_devices":
		return r.runDeleteDevices()
	case "delete_homes":
		return r.runDeleteHomes()
	case "delete_users_self":
		return r.runDeleteUsersSelf()
	case "delete_users_admin":
		return r.runDeleteUsersAdmin()
	default:
		return fmt.Errorf("unknown phase %s", r.cfg.Phase.Name)
	}
}

func (r *Runner) runCreateUsers() error {
	err := r.runScheduled(func(ctx context.Context, index int) error {
		credentials := userCredentials(r.cfg.RunID, index)
		created, ok := r.registerUser(ctx, credentials)
		if !ok {
			return nil
		}
		r.metrics.RecordCounter("phase_users_created", 1, r.scenario)
		userIDCheck := created.UserID > 0
		r.metrics.RecordCheck(r.scenario, "Create users returned a user id", userIDCheck)
		if !userIDCheck {
			return nil
		}

		_, sessionToken := r.loginSession(ctx, credentials.Username, credentials.Password)
		if sessionToken == "" {
			return nil
		}
		code, ok := r.authorizeCode(ctx, sessionToken, credentials.Username+"-bootstrap")
		if !ok {
			return nil
		}
		token, ok := r.exchangeOAuthToken(ctx, code)
		if !ok {
			return nil
		}
		r.state.UpsertUser(index, created.UserID, credentials.Username, credentials.Password, sessionToken, token.AccessToken, token.RefreshToken)
		return nil
	})
	if err != nil {
		return err
	}
	return r.evaluateThresholds()
}

func (r *Runner) runGoogleOAuthEnroll() error {
	err := r.runScheduled(func(ctx context.Context, index int) error {
		session, ok := r.ensureSessionForSlot(ctx, index)
		if !ok {
			return nil
		}

		code, ok := r.authorizeCode(ctx, session.SessionToken, fmt.Sprintf("%d-google-oauth-enroll", index))
		if !ok {
			return nil
		}
		r.metrics.RecordCounter("phase_google_oauth_authorized", 1, r.scenario)

		token, ok := r.exchangeOAuthToken(ctx, code)
		if !ok {
			return nil
		}
		r.metrics.RecordCounter("phase_google_oauth_token_exchanged", 1, r.scenario)

		r.state.UpdateOAuth(index, token.AccessToken, token.RefreshToken)

		// Validate token by issuing a SYNC fulfillment call
		_, ok = r.googleFulfillment(ctx, token.AccessToken, "action.devices.SYNC", map[string]any{})
		r.metrics.RecordCheck(r.scenario, "Google OAuth SYNC validation succeeded", ok)
		if ok {
			r.metrics.RecordCounter("phase_google_oauth_enrolled", 1, r.scenario)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return r.evaluateThresholds()
}

func (r *Runner) runGoogleSignInEnroll() error {
	err := r.runScheduled(func(ctx context.Context, index int) error {
		googleSub := fmt.Sprintf("google-sub-%s-%d", r.cfg.RunID, index)
		email := fmt.Sprintf("stress_gsign_%s_%d@example.com", r.cfg.RunID, index)

		parsed, sessionToken, ok := r.testGoogleLogin(ctx, googleSub, email)
		if !ok {
			return nil
		}
		r.metrics.RecordCounter("phase_google_signin_created", 1, r.scenario)

		userIDCheck := parsed.UserID > 0
		r.metrics.RecordCheck(r.scenario, "Google sign-in returned a user id", userIDCheck)
		if !userIDCheck {
			return nil
		}

		// Authorize + exchange OAuth token to verify the full flow works
		code, ok := r.authorizeCode(ctx, sessionToken, fmt.Sprintf("%d-google-signin-enroll", index))
		if !ok {
			return nil
		}
		_, ok = r.exchangeOAuthToken(ctx, code)
		if !ok {
			return nil
		}

		// Do NOT call UpsertUser — these Google users are separate from the
		// password-based users created by create_users. Overwriting shared
		// state would break downstream phases (delete_users_self etc.).
		r.metrics.RecordCounter("phase_google_signin_enrolled", 1, r.scenario)
		return nil
	})
	if err != nil {
		return err
	}
	return r.evaluateThresholds()
}

func (r *Runner) runCreateHomes() error {
	var homesMu sync.Mutex
	createdHomes := make([]createdHome, 0, r.cfg.Phase.TotalItems)
	err := r.runScheduled(func(ctx context.Context, index int) error {
		userIndex := index / r.cfg.HomesPerUser
		homeSlot := index % r.cfg.HomesPerUser
		session, ok := r.ensureSessionForSlot(ctx, userIndex)
		if !ok {
			return nil
		}
		home, createdAt, ok := r.enrollHome(ctx, session.SessionToken, userIndex, homeSlot)
		if !ok {
			return nil
		}
		r.metrics.RecordCounter("phase_homes_created", 1, r.scenario)
		name := homeName(r.cfg.RunID, userIndex, homeSlot)
		r.state.UpsertHome(userIndex, homeSlot, home.HomeID, name)
		homesMu.Lock()
		createdHomes = append(createdHomes, createdHome{
			UserIndex: userIndex,
			HomeSlot:  homeSlot,
			HomeID:    home.HomeID,
			Name:      name,
			CreatedAt: createdAt,
		})
		homesMu.Unlock()
		return nil
	})
	if err != nil {
		return err
	}

	if len(createdHomes) > 0 {
		r.logf("Awaiting async readiness for %d homes", len(createdHomes))
		if barrierErr := r.awaitHomesReady(createdHomes); barrierErr != nil {
			return barrierErr
		}
	}
	return r.evaluateThresholds()
}

func (r *Runner) runEnrollDevices() error {
	products, err := r.stressProducts()
	if err != nil {
		return err
	}
	devicesPerUser := r.cfg.HomesPerUser * r.cfg.DevicesPerHome
	err = r.runScheduled(func(ctx context.Context, index int) error {
		userIndex := index / devicesPerUser
		withinUser := index % devicesPerUser
		homeSlot := withinUser / r.cfg.DevicesPerHome
		deviceSlot := withinUser % r.cfg.DevicesPerHome
		session, ok := r.ensureSessionForSlot(ctx, userIndex)
		if !ok {
			return nil
		}
		home, ok := r.state.Home(userIndex, homeSlot)
		if !ok || home.HomeID <= 0 {
			r.metrics.RecordCounter("phase_state_missing", 1, r.scenario)
			r.logf("missing phase-state home slot %d/%d", userIndex, homeSlot)
			return nil
		}
		product := products[withinUser%len(products)]
		device, ok := r.enrollDevice(ctx, session.SessionToken, home.HomeID, product, userIndex, homeSlot, deviceSlot)
		if !ok {
			return nil
		}
		deviceIDCheck := device.DeviceID > 0
		r.metrics.RecordCheck(r.scenario, "Enroll devices returned a device id", deviceIDCheck)
		if !deviceIDCheck {
			return nil
		}
		r.metrics.RecordCounter("phase_devices_enrolled", 1, r.scenario)
		r.state.UpsertDevice(userIndex, homeSlot, deviceSlot, device.DeviceID, product.ProductID, product.Name, fmt.Sprintf("%d:power", device.DeviceID), deviceName(r.cfg.RunID, userIndex, homeSlot, deviceSlot))
		return nil
	})
	if err != nil {
		return err
	}
	return r.evaluateThresholds()
}

func (r *Runner) runFulfillmentMix() error {
	limit := r.cfg.FulfillmentDeviceLimit
	if limit <= 0 {
		limit = r.cfg.UserCount * r.cfg.HomesPerUser * r.cfg.DevicesPerHome
	}
	devicesPerUser := r.cfg.HomesPerUser * r.cfg.DevicesPerHome
	err := r.runScheduled(func(ctx context.Context, index int) error {
		if index >= limit {
			return nil
		}
		userIndex := index / devicesPerUser
		withinUser := index % devicesPerUser
		homeSlot := withinUser / r.cfg.DevicesPerHome
		deviceSlot := withinUser % r.cfg.DevicesPerHome

		session, ok := r.ensureSessionForSlot(ctx, userIndex)
		if !ok {
			return nil
		}
		device, ok := r.state.Device(userIndex, homeSlot, deviceSlot)
		if !ok || device.CompoundID == "" {
			r.metrics.RecordCounter("phase_state_missing", 1, r.scenario)
			r.logf("missing phase-state device slot %d/%d/%d", userIndex, homeSlot, deviceSlot)
			return nil
		}
		accessToken, ok := r.ensureOAuthForSlot(ctx, userIndex, session.SessionToken)
		if !ok {
			return nil
		}
		for _, step := range fulfillmentSteps(device.CompoundID, withinUser, r.cfg.FulfillmentRequestsPerDevice) {
			payload, ok := r.googleFulfillment(ctx, accessToken, step.Intent, step.Payload)
			if !ok {
				return nil
			}
			switch step.Intent {
			case "action.devices.SYNC":
				ok = syncResponseIncludesDevice(payload, device.CompoundID)
				r.metrics.RecordCheck(r.scenario, "SYNC includes the target device capability", ok)
			case "action.devices.QUERY":
				ok = queryResponseSucceeded(payload, device.CompoundID)
				r.metrics.RecordCheck(r.scenario, "QUERY returns success for the target device", ok)
			case "action.devices.EXECUTE":
				ok = executeResponseSucceeded(payload)
				r.metrics.RecordCheck(r.scenario, "EXECUTE returns success", ok)
			default:
				ok = true
			}
			if !ok {
				return nil
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return r.evaluateThresholds()
}

func (r *Runner) runDeleteDevices() error {
	err := r.runScheduled(func(ctx context.Context, index int) error {
		userIndex := index / r.cfg.DeleteDevicesPerUser
		deviceSlot := index % r.cfg.DeleteDevicesPerUser
		session, ok := r.ensureSessionForSlot(ctx, userIndex)
		if !ok {
			return nil
		}
		device, ok := r.state.Device(userIndex, r.cfg.DeleteHomeSlot, deviceSlot)
		if !ok || device.DeviceID <= 0 {
			r.metrics.RecordCounter("phase_state_missing", 1, r.scenario)
			r.logf("missing phase-state delete-device slot %d/%d/%d", userIndex, r.cfg.DeleteHomeSlot, deviceSlot)
			return nil
		}
		if r.deleteDevice(ctx, session.SessionToken, device.DeviceID) {
			r.metrics.RecordCounter("phase_devices_deleted", 1, r.scenario)
			r.state.MarkDeviceDeleted(userIndex, r.cfg.DeleteHomeSlot, deviceSlot)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return r.evaluateThresholds()
}

func (r *Runner) runDeleteHomes() error {
	err := r.runScheduled(func(ctx context.Context, index int) error {
		userIndex := index / r.cfg.DeleteHomesPerUser
		homeSlot := r.cfg.DeleteHomeSlot + (index % r.cfg.DeleteHomesPerUser)
		session, ok := r.ensureSessionForSlot(ctx, userIndex)
		if !ok {
			return nil
		}
		home, ok := r.state.Home(userIndex, homeSlot)
		if !ok || home.HomeID <= 0 {
			r.metrics.RecordCounter("phase_state_missing", 1, r.scenario)
			r.logf("missing phase-state delete-home slot %d/%d", userIndex, homeSlot)
			return nil
		}
		deletion, ok := r.deleteHome(ctx, session.SessionToken, home.HomeID)
		if !ok {
			return nil
		}
		isDeleting := deletion.MQTTProvisionState != "" && strings.EqualFold(deletion.MQTTProvisionState, "deleting")
		r.metrics.RecordCheck(r.scenario, "Delete homes marks the home as deleting immediately", isDeleting)
		if isDeleting {
			r.metrics.RecordCounter("phase_homes_delete_requested", 1, r.scenario)
			r.state.MarkHomeDeleteRequested(userIndex, homeSlot)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return r.evaluateThresholds()
}

func (r *Runner) runDeleteUsersSelf() error {
	err := r.runScheduled(func(ctx context.Context, index int) error {
		userIndex := r.cfg.DeleteUsersSelfStartIndex + index
		session, ok := r.ensureSessionForSlot(ctx, userIndex)
		if !ok {
			return nil
		}
		if !r.deleteCurrentUser(ctx, session.SessionToken) {
			return nil
		}
		r.metrics.RecordCounter("phase_users_deleted_self", 1, r.scenario)
		r.state.MarkUserDeleted(userIndex, "self")
		_, sessionToken := r.loginSession(ctx, session.Username, session.Password, 401)
		rejected := sessionToken == ""
		r.metrics.RecordCheck(r.scenario, "Self-deleted users cannot log in again", rejected)
		return nil
	})
	if err != nil {
		return err
	}
	return r.evaluateThresholds()
}

func (r *Runner) runDeleteUsersAdmin() error {
	err := r.runScheduled(func(ctx context.Context, index int) error {
		userIndex := r.cfg.DeleteUsersAdminStartIndex + index
		session, ok := r.ensureSessionForSlot(ctx, userIndex)
		if !ok || session.User.UserID <= 0 {
			return nil
		}
		_, adminSession := r.loginSession(ctx, r.cfg.AdminUsername, r.cfg.AdminPassword)
		if adminSession == "" {
			return nil
		}
		defer r.logoutSession(context.Background(), adminSession, true)
		if !r.deleteUserAsAdmin(ctx, adminSession, session.User.UserID) {
			return nil
		}
		r.metrics.RecordCounter("phase_users_deleted_admin", 1, r.scenario)
		r.state.MarkUserDeleted(userIndex, "admin")
		_, sessionToken := r.loginSession(ctx, session.Username, session.Password, 401)
		rejected := sessionToken == ""
		r.metrics.RecordCheck(r.scenario, "Admin-deleted users cannot log in again", rejected)
		return nil
	})
	if err != nil {
		return err
	}
	return r.evaluateThresholds()
}

func (r *Runner) runScheduled(process func(context.Context, int) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), r.cfg.Phase.MaxDuration)
	defer cancel()

	progress := &phaseProgress{}
	stopLogs := make(chan struct{})
	go r.progressLogger(stopLogs, progress, r.cfg.Phase.TotalItems)

	jobs := make(chan int, max(1, r.cfg.Phase.Workers*2))
	errCh := make(chan error, 1)
	var wg sync.WaitGroup
	for worker := 0; worker < r.cfg.Phase.Workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				if err := process(ctx, index); err != nil {
					select {
					case errCh <- err:
					default:
					}
					cancel()
					return
				}
				progress.completed.Add(1)
			}
		}()
	}

	pacer := newPhasePacer(r.cfg.Phase.StartRPS, r.cfg.Phase.PeakRPS, r.cfg.Phase.RampUp, r.cfg.Phase.Hold, r.cfg.Phase.RampDown)
	beganAt := time.Now()
	for hit := 0; hit < r.cfg.Phase.TotalItems; hit++ {
		wait, stop := pacer.Pace(time.Since(beganAt), uint64(hit))
		if stop {
			close(jobs)
			wg.Wait()
			close(stopLogs)
			return fmt.Errorf("phase %s schedule ended before dispatching %d of %d items", r.cfg.Phase.Name, hit, r.cfg.Phase.TotalItems)
		}
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				close(jobs)
				wg.Wait()
				close(stopLogs)
				if errors.Is(ctx.Err(), context.DeadlineExceeded) {
					return fmt.Errorf("phase %s exceeded max duration before dispatch finished", r.cfg.Phase.Name)
				}
				return ctx.Err()
			case err := <-errCh:
				timer.Stop()
				close(jobs)
				wg.Wait()
				close(stopLogs)
				return err
			case <-timer.C:
			}
		}

		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			close(stopLogs)
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return fmt.Errorf("phase %s exceeded max duration before dispatch finished", r.cfg.Phase.Name)
			}
			return ctx.Err()
		case err := <-errCh:
			close(jobs)
			wg.Wait()
			close(stopLogs)
			return err
		case jobs <- hit:
			progress.submitted.Add(1)
		}
	}

	close(jobs)
	wg.Wait()
	close(stopLogs)

	select {
	case err := <-errCh:
		return err
	default:
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("phase %s exceeded max duration", r.cfg.Phase.Name)
	}
	return nil
}

func (r *Runner) awaitHomesReady(createdHomes []createdHome) error {
	barrierStartedAt := time.Now()
	grouped := map[int][]*trackedHome{}
	for _, home := range createdHomes {
		tracked := &trackedHome{createdHome: home}
		grouped[home.UserIndex] = append(grouped[home.UserIndex], tracked)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 1)
	for userIndex, homes := range grouped {
		userIndex := userIndex
		homes := homes
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := r.awaitHomesForUser(userIndex, homes, barrierStartedAt); err != nil {
				select {
				case errCh <- err:
				default:
				}
			}
		}()
	}
	wg.Wait()
	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

func (r *Runner) awaitHomesForUser(userIndex int, homes []*trackedHome, barrierStartedAt time.Time) error {
	user, ok := r.state.User(userIndex)
	if !ok || user.SessionToken == "" {
		r.metrics.RecordCounter("phase_state_missing", 1, r.scenario)
		return nil
	}
	barrierDeadline := barrierStartedAt.Add(r.cfg.AsyncHomeReadyTimeout)
	ctx, cancel := context.WithDeadline(context.Background(), barrierDeadline)
	defer cancel()

	pending := len(homes)
	for pending > 0 {
		now := time.Now()
		homesList, listOK := r.listHomes(ctx, user.SessionToken)
		if listOK {
			for _, home := range homes {
				if home.done {
					continue
				}
				home.polls++
				entry := findHomeByID(homesList, home.HomeID)

				if !home.earlyChecked && r.cfg.AsyncHomeEarlyReadyCheck > 0 && now.Sub(home.CreatedAt) >= r.cfg.AsyncHomeEarlyReadyCheck {
					ok := entry != nil && provisionState(*entry) != "ready"
					home.earlyChecked = true
					if !ok {
						r.metrics.RecordCounter("async_home_ready_early", 1, r.scenario)
					}
				}

				if entry != nil {
					state := provisionState(*entry)
					switch state {
					case "ready":
						r.recordAsyncHomeResolution("async_home_ready_success", home.CreatedAt, home.polls)
						lookupOK := findHomeByName(homesList, home.Name) != nil
						r.metrics.RecordCheck(r.scenario, "Create homes can rediscover the home by name", lookupOK)
						home.done = true
						pending--
					case "failed", "deleting":
						r.recordAsyncHomeResolution("async_home_ready_failed_state", home.CreatedAt, home.polls)
						home.done = true
						pending--
					}
				}

				if !home.done && !now.Before(barrierDeadline) {
					r.recordAsyncHomeResolution("async_home_ready_timeout", home.CreatedAt, home.polls)
					home.done = true
					pending--
				}
			}
		} else {
			for _, home := range homes {
				if home.done {
					continue
				}
				if now.Sub(home.CreatedAt) >= r.cfg.AsyncHomeReadyTimeout {
					r.recordAsyncHomeResolution("async_home_ready_timeout", home.CreatedAt, home.polls)
					home.done = true
					pending--
				}
			}
		}

		if pending == 0 {
			break
		}
		select {
		case <-ctx.Done():
			for _, home := range homes {
				if home.done {
					continue
				}
				r.recordAsyncHomeResolution("async_home_ready_timeout", home.CreatedAt, home.polls)
				home.done = true
			}
			return nil
		case <-time.After(r.cfg.AsyncHomeReadyPollInterval):
		}
	}
	return nil
}

func (r *Runner) recordAsyncHomeResolution(metric string, createdAt time.Time, polls int) {
	elapsedMs := time.Since(createdAt).Seconds() * 1000
	r.metrics.RecordTrend("async_home_ready_duration", elapsedMs, r.scenario)
	r.metrics.RecordTrend("async_home_ready_polls", float64(polls), r.scenario)
	r.metrics.RecordCounter(metric, 1, r.scenario)
}

func (r *Runner) stressProducts() ([]ProductInfo, error) {
	products := r.state.Products()
	if len(products) < r.cfg.ExpectedProductCount {
		return nil, fmt.Errorf("expected at least %d products with prefix %s, found %d", r.cfg.ExpectedProductCount, r.cfg.ProductPrefix, len(products))
	}
	sort.Slice(products, func(i, j int) bool {
		return products[i].Name < products[j].Name
	})
	return products[:r.cfg.ExpectedProductCount], nil
}

func (r *Runner) evaluateThresholds() error {
	r.metrics.mu.Lock()
	defer r.metrics.mu.Unlock()
	if ratio(r.metrics.httpFailed, r.metrics.httpReqs) >= 0.10 {
		return fmt.Errorf("threshold crossed: http_req_failed rate %.2f%%", ratio(r.metrics.httpFailed, r.metrics.httpReqs)*100)
	}
	if r.metrics.counters["async_home_ready_timeout"] > 0 {
		return fmt.Errorf("threshold crossed: async_home_ready_timeout count %.0f", r.metrics.counters["async_home_ready_timeout"])
	}
	if r.metrics.counters["async_home_ready_failed_state"] > 0 {
		return fmt.Errorf("threshold crossed: async_home_ready_failed_state count %.0f", r.metrics.counters["async_home_ready_failed_state"])
	}
	if r.metrics.counters["phase_state_missing"] > 0 {
		return fmt.Errorf("phase state missing count %.0f", r.metrics.counters["phase_state_missing"])
	}
	if r.metrics.checkFails > 0 {
		return fmt.Errorf("checks failed: %d", r.metrics.checkFails)
	}
	return nil
}

func (r *Runner) progressLogger(stop <-chan struct{}, progress *phaseProgress, totalItems int) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			submitted := progress.submitted.Load()
			completed := progress.completed.Load()
			r.logf("Phase %s progress: dispatched %d/%d, completed %d/%d", r.cfg.Phase.Name, submitted, totalItems, completed, totalItems)
		}
	}
}

func minDuration(left, right time.Duration) time.Duration {
	if left < right {
		return left
	}
	return right
}

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}
