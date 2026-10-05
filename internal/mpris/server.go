package mpris

import (
	"fmt"
	"strings"
	"sync"

	"github.com/Chavao/rukia-player/internal/spotify"
	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"
)

const (
	// BusNameRukiaPlayer is the canonical MPRIS bus name for rukia-player.
	BusNameRukiaPlayer = "org.mpris.MediaPlayer2.rukia-player"
	// BusNameRukia is an alternative bus name for playerctl matching.
	BusNameRukia = "org.mpris.MediaPlayer2.rukia"
	// BusNameGoLibrespot is an alias matching pavucontrol output.
	BusNameGoLibrespot = "org.mpris.MediaPlayer2.go-librespot"

	// ObjectPath is the standard MPRIS object path.
	ObjectPath = "/org/mpris/MediaPlayer2"
	// InterfaceRoot is the base MPRIS interface.
	InterfaceRoot = "org.mpris.MediaPlayer2"
	// InterfacePlayer is the playback control MPRIS interface.
	InterfacePlayer = "org.mpris.MediaPlayer2.Player"
)

// Playback control messages sent from MPRIS to the application event loop.
type (
	// TogglePlayPauseMsg requests toggling between play and pause.
	TogglePlayPauseMsg struct{}
	// PlayMsg requests resuming playback.
	PlayMsg struct{}
	// PauseMsg requests pausing playback.
	PauseMsg struct{}
	// StopMsg requests stopping playback.
	StopMsg struct{}
	// NextMsg requests skipping to the next track.
	NextMsg struct{}
	// PreviousMsg requests skipping to the previous track.
	PreviousMsg struct{}
	// VolumeMsg requests updating volume to a percentage (0-100).
	VolumeMsg struct{ Percent int }
	// QuitMsg requests terminating the player application.
	QuitMsg struct{}
)

// Server implements the MPRIS v2 D-Bus specification for rukia-player.
type Server struct {
	conn      *dbus.Conn
	props     *prop.Properties
	mu        sync.Mutex
	sender    func(any)
	closed    bool
	closeOnce sync.Once
	names     []string
	rootIface *rootInterface
	playIface *playerInterface
}

// rootInterface implements org.mpris.MediaPlayer2.
type rootInterface struct {
	server *Server
}

// Raise brings the player window to the front (no-op in CLI).
func (r *rootInterface) Raise() *dbus.Error {
	return nil
}

// Quit requests the media player to terminate.
func (r *rootInterface) Quit() *dbus.Error {
	r.server.send(QuitMsg{})
	return nil
}

// playerInterface implements org.mpris.MediaPlayer2.Player.
type playerInterface struct {
	server *Server
}

// Next skips to the next track.
func (p *playerInterface) Next() *dbus.Error {
	p.server.send(NextMsg{})
	return nil
}

// Previous skips to the previous track.
func (p *playerInterface) Previous() *dbus.Error {
	p.server.send(PreviousMsg{})
	return nil
}

// Pause pauses playback.
func (p *playerInterface) Pause() *dbus.Error {
	p.server.send(PauseMsg{})
	return nil
}

// PlayPause toggles play/pause.
func (p *playerInterface) PlayPause() *dbus.Error {
	p.server.send(TogglePlayPauseMsg{})
	return nil
}

// Stop stops playback.
func (p *playerInterface) Stop() *dbus.Error {
	p.server.send(StopMsg{})
	return nil
}

// Play starts or resumes playback.
func (p *playerInterface) Play() *dbus.Error {
	p.server.send(PlayMsg{})
	return nil
}

// SeekBy is the MPRIS Seek method, named SeekBy in Go to avoid conflicting with io.Seeker.
func (p *playerInterface) SeekBy(offsetUs int64) *dbus.Error {
	return nil
}

// SetPosition sets the position in microseconds (unimplemented).
func (p *playerInterface) SetPosition(trackID dbus.ObjectPath, positionUs int64) *dbus.Error {
	return nil
}

// OpenUri opens the specified URI (unimplemented).
func (p *playerInterface) OpenUri(uri string) *dbus.Error {
	return nil
}

// NewServer initializes and registers the MPRIS D-Bus server on the session bus.
func NewServer() (*Server, error) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return nil, fmt.Errorf("connecting to session bus: %w", err)
	}

	names := []string{BusNameRukiaPlayer, BusNameRukia, BusNameGoLibrespot}
	var claimed []string
	for _, name := range names {
		reply, reqErr := conn.RequestName(name, dbus.NameFlagReplaceExisting)
		if reqErr == nil && reply == dbus.RequestNameReplyPrimaryOwner {
			claimed = append(claimed, name)
		}
	}
	if len(claimed) == 0 {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to claim any MPRIS bus name (%v)", names)
	}

	s := &Server{
		conn:  conn,
		names: claimed,
	}
	s.rootIface = &rootInterface{server: s}
	s.playIface = &playerInterface{server: s}

	if err := conn.Export(s.rootIface, ObjectPath, InterfaceRoot); err != nil {
		s.Close()
		return nil, fmt.Errorf("exporting %s: %w", InterfaceRoot, err)
	}
	playerMethods := map[string]string{"SeekBy": "Seek"}
	if err := conn.ExportWithMap(s.playIface, playerMethods, ObjectPath, InterfacePlayer); err != nil {
		s.Close()
		return nil, fmt.Errorf("exporting %s: %w", InterfacePlayer, err)
	}

	propsSpec := map[string]map[string]*prop.Prop{
		InterfaceRoot: {
			"CanQuit":             {Value: true, Writable: false, Emit: prop.EmitConst},
			"CanRaise":            {Value: false, Writable: false, Emit: prop.EmitConst},
			"HasTrackList":        {Value: false, Writable: false, Emit: prop.EmitConst},
			"Identity":            {Value: "rukia-player", Writable: false, Emit: prop.EmitConst},
			"SupportedUriSchemes": {Value: []string{"spotify"}, Writable: false, Emit: prop.EmitConst},
			"SupportedMimeTypes":  {Value: []string{}, Writable: false, Emit: prop.EmitConst},
		},
		InterfacePlayer: {
			"PlaybackStatus": {Value: "Stopped", Writable: false, Emit: prop.EmitTrue},
			"LoopStatus":     {Value: "None", Writable: false, Emit: prop.EmitTrue},
			"Rate":           {Value: 1.0, Writable: false, Emit: prop.EmitConst},
			"Shuffle":        {Value: false, Writable: false, Emit: prop.EmitTrue},
			"Metadata":       {Value: makeMetadata(nil), Writable: false, Emit: prop.EmitTrue},
			"Volume":         {Value: 1.0, Writable: true, Emit: prop.EmitTrue, Callback: s.onVolumeChange},
			"Position":       {Value: int64(0), Writable: false, Emit: prop.EmitFalse},
			"MinimumRate":    {Value: 1.0, Writable: false, Emit: prop.EmitConst},
			"MaximumRate":    {Value: 1.0, Writable: false, Emit: prop.EmitConst},
			"CanGoNext":      {Value: true, Writable: false, Emit: prop.EmitConst},
			"CanGoPrevious":  {Value: true, Writable: false, Emit: prop.EmitConst},
			"CanPlay":        {Value: true, Writable: false, Emit: prop.EmitConst},
			"CanPause":       {Value: true, Writable: false, Emit: prop.EmitConst},
			"CanSeek":        {Value: false, Writable: false, Emit: prop.EmitConst},
			"CanControl":     {Value: true, Writable: false, Emit: prop.EmitConst},
		},
	}

	properties, err := prop.Export(conn, ObjectPath, propsSpec)
	if err != nil {
		s.Close()
		return nil, fmt.Errorf("exporting properties: %w", err)
	}
	s.props = properties

	return s, nil
}

// SetSender sets the dispatch function to send events to Bubble Tea.
func (s *Server) SetSender(sender func(any)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sender = sender
}

func (s *Server) send(msg any) {
	s.mu.Lock()
	fn := s.sender
	s.mu.Unlock()
	if fn != nil {
		fn(msg)
	}
}

func (s *Server) onVolumeChange(c *prop.Change) *dbus.Error {
	vol, ok := c.Value.(float64)
	if !ok {
		return prop.ErrInvalidArg
	}
	percent := int(vol * 100)
	if percent < 0 {
		percent = 0
	} else if percent > 100 {
		percent = 100
	}
	s.send(VolumeMsg{Percent: percent})
	return nil
}

func (s *Server) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// UpdatePlaybackState updates all MPRIS properties according to the current player state.
func (s *Server) UpdatePlaybackState(status string, track *spotify.Track, volume int, shuffle bool, repeat string, positionMs int) {
	if s.isClosed() || s.props == nil {
		return
	}

	s.props.SetMust(InterfacePlayer, "PlaybackStatus", formatStatus(status))
	s.props.SetMust(InterfacePlayer, "LoopStatus", formatLoopStatus(repeat))
	s.props.SetMust(InterfacePlayer, "Shuffle", shuffle)
	s.props.SetMust(InterfacePlayer, "Metadata", makeMetadata(track))
	s.props.SetMust(InterfacePlayer, "Volume", float64(volume)/100.0)
	s.props.SetMust(InterfacePlayer, "Position", int64(positionMs)*1000)
}

// UpdateStatus updates the PlaybackStatus property ("Playing", "Paused", "Stopped").
func (s *Server) UpdateStatus(status string) {
	if s.isClosed() || s.props == nil {
		return
	}
	s.props.SetMust(InterfacePlayer, "PlaybackStatus", formatStatus(status))
}

// UpdateVolume updates the Volume property (0-100 mapped to 0.0-1.0).
func (s *Server) UpdateVolume(volume int) {
	if s.isClosed() || s.props == nil {
		return
	}
	s.props.SetMust(InterfacePlayer, "Volume", float64(volume)/100.0)
}

// UpdateTrack updates the Metadata property for the current track.
func (s *Server) UpdateTrack(track *spotify.Track) {
	if s.isClosed() || s.props == nil {
		return
	}
	s.props.SetMust(InterfacePlayer, "Metadata", makeMetadata(track))
}

// UpdateShuffle updates the Shuffle property.
func (s *Server) UpdateShuffle(shuffle bool) {
	if s.isClosed() || s.props == nil {
		return
	}
	s.props.SetMust(InterfacePlayer, "Shuffle", shuffle)
}

// UpdateRepeat updates the LoopStatus property.
func (s *Server) UpdateRepeat(repeat string) {
	if s.isClosed() || s.props == nil {
		return
	}
	s.props.SetMust(InterfacePlayer, "LoopStatus", formatLoopStatus(repeat))
}

// Close releases registered D-Bus names and closes the session connection.
func (s *Server) Close() error {
	var err error
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()

		for _, name := range s.names {
			_, _ = s.conn.ReleaseName(name)
		}
		err = s.conn.Close()
	})
	return err
}

func formatStatus(status string) string {
	switch strings.ToLower(status) {
	case "playing":
		return "Playing"
	case "paused":
		return "Paused"
	default:
		return "Stopped"
	}
}

func formatLoopStatus(repeat string) string {
	switch repeat {
	case "track":
		return "Track"
	case "context":
		return "Playlist"
	default:
		return "None"
	}
}

func makeMetadata(track *spotify.Track) map[string]any {
	meta := map[string]any{
		"mpris:trackid": dbus.ObjectPath("/org/mpris/MediaPlayer2/TrackList/NoTrack"),
		"xesam:title":   "",
		"xesam:artist":  []string{},
		"xesam:album":   "",
		"mpris:length":  int64(0),
	}
	if track == nil {
		return meta
	}

	trackID := sanitizeDbusPath(track.ID)
	if trackID != "" {
		meta["mpris:trackid"] = dbus.ObjectPath("/org/mpris/MediaPlayer2/Track/" + trackID)
		meta["xesam:url"] = "https://open.spotify.com/track/" + track.ID
	}
	meta["xesam:title"] = track.Name
	if track.Artist != "" {
		meta["xesam:artist"] = []string{track.Artist}
		meta["xesam:albumArtist"] = []string{track.Artist}
	}
	meta["xesam:album"] = track.Album
	meta["mpris:length"] = int64(track.DurationMs) * 1000
	if track.PlaylistPosition >= 0 {
		meta["xesam:trackNumber"] = int32(track.PlaylistPosition + 1)
	}
	return meta
}

func sanitizeDbusPath(id string) string {
	var sb strings.Builder
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}
