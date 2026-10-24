// Package gamemsg encodes and decodes the Fall Guys game messages a round server exchanges
// with the client, on top of internal/hlapi. A message is [57 + type][fields]; the field order
// of each one is the client's own Serialize/Deserialize (docs/protocol.md, "Round server").
//
// Only what the round server uses is here. Field names follow the client's.
package gamemsg

import (
	"encoding/binary"
	"errors"
	"math"

	"openpak/fall-guys/internal/hlapi"
)

// Type is EnumGameMessageType.
type Type byte

const (
	ClientConnectInitial  Type = 0
	ClientConnectClient   Type = 1
	ClientDisconnect      Type = 2
	ClientMotorTasks      Type = 4
	ClientObjectTransform Type = 5
	ClientReadyRound      Type = 6
	ClientRequestGeneric  Type = 7
	ClientRequestSpawn    Type = 8
	ClientSetReady        Type = 9
	ClientResetCheckpoint Type = 12
	ClientSkipRound       Type = 13

	ServerConnectInitial    Type = 17
	ServerConnectedClient   Type = 18
	ServerEndRound          Type = 22
	ServerEventGeneric      Type = 23
	ServerGameDataOptions   Type = 25
	ServerMotorAgentState   Type = 27
	ServerMotorTasks        Type = 28
	ServerObjectTransform   Type = 29
	ServerPlayerProgress    Type = 31
	ServerQualification     Type = 32
	ServerReadyRoundResp    Type = 33
	ServerRoundResults      Type = 34
	ServerSpawnObject       Type = 36
	ServerStartGame         Type = 37
	ServerStartLoadingLevel Type = 38
	ServerUnspawnObject     Type = 42

	Ping Type = 48
	Pong Type = 49
)

// MsgID is the HLAPI message id of a type.
func (t Type) MsgID() byte { return byte(t) + hlapi.GameBase }

// TypeOf maps an HLAPI message id back; ok is false for HLAPI's own ids.
func TypeOf(msgID byte) (Type, bool) {
	if msgID < hlapi.GameBase || msgID >= hlapi.GameBase+50 {
		return 0, false
	}
	return Type(msgID - hlapi.GameBase), true
}

func start(t Type) *hlapi.Writer {
	w := &hlapi.Writer{}
	w.Start(t.MsgID())
	return w
}

// ShortString is UnityNetworkingExtensionClass.WriteShortString: packed byte count + UTF-8.
func ShortString(w *hlapi.Writer, s string) {
	w.Packed(uint32(len(s)))
	w.Bytes([]byte(s))
}

func readShortString(r *hlapi.Reader) string {
	return string(r.Bytes(int(r.Packed())))
}

// Guid is 16 bytes in .NET ToByteArray order.
type Guid [16]byte

// ---------------------------------------------------------------------------------------------
// Connect handshake

// ProtocolVersion the 11.6.1 client embeds in its CLIENT_CONNECT_INITIAL.
const ProtocolVersion uint64 = 0x0174264009500512

const connectMarker = 0x03E6EB23

// ConnectInitial is what CLIENT_CONNECT_INITIAL carries.
type ConnectInitial struct {
	Protocol  uint64 // decoded from the low bytes of the embedded ints
	SupportID string
	Build     string
}

// DecodeConnectInitial reads the body after the message id.
func DecodeConnectInitial(r *hlapi.Reader) (ConnectInitial, error) {
	var c ConnectInitial
	var pv []byte
	if r.I32() != connectMarker {
		return c, errors.New("gamemsg: connect initial: no marker")
	}
	n := int(r.I32()) - 3
	if n < 0 || n > 64 {
		return c, errors.New("gamemsg: connect initial: bad count")
	}
	for i := 0; i < n; i++ {
		pv = append(pv, byte(r.I32()))
	}
	c.SupportID = r.String()
	c.Build = r.String()
	m := int(r.I32()) - connectMarker
	if m < 0 || m > 64 {
		return c, errors.New("gamemsg: connect initial: bad second count")
	}
	for i := 0; i < m; i++ {
		pv = append(pv, byte(r.I32()))
	}
	if len(pv) >= 8 {
		c.Protocol = binary.LittleEndian.Uint64(pv[:8])
	}
	return c, r.Err
}

// EncodeServerConnectInitial: int k, k junk floats, the key.
func EncodeServerConnectInitial(key uint64) []byte {
	w := start(ServerConnectInitial)
	w.I32(2)
	w.F32(0)
	w.F32(0)
	w.U64(key)
	return w.B
}

// ConnectProof is what the client answers a key with.
func ConnectProof(key uint64) uint64 { return (key << (key % 30)) & key }

// ConnectClient is CLIENT_CONNECT_CLIENT.
type ConnectClient struct {
	Proof          uint64
	NetworkID      uint64
	ClientType     byte
	EntryToken     string
	WantsSpectate  bool
	CrownRank      byte
	LocalPlayers   byte
	GameStateData  string
	Flag1, Flag2   bool
	AppVersion     string
	ContentVersion string
}

func DecodeConnectClient(r *hlapi.Reader) (ConnectClient, error) {
	c := ConnectClient{
		Proof: r.U64(), NetworkID: r.U64(), ClientType: r.Byte(), EntryToken: readShortString(r),
		WantsSpectate: r.Bool(), CrownRank: r.Byte(), LocalPlayers: r.Byte(),
	}
	c.GameStateData = readShortString(r)
	c.Flag1, c.Flag2 = r.Bool(), r.Bool()
	c.AppVersion = readShortString(r)
	c.ContentVersion = readShortString(r)
	return c, r.Err
}

// Connection response codes (EnumConnectionResponse).
const (
	ConnectParticipant   int32 = 0
	ConnectSpectator     int32 = 1
	ConnectAlreadyIn     int32 = 2
	ConnectFailedCheck   int32 = 3
	ConnectTokenInvalid  int32 = 5
	ConnectServerNotRdy  int32 = 9
	ConnectGameStarted   int32 = 10
	ConnectSlotNotReserv int32 = 12
)

// ConnectedClient is SERVER_CONNECTED_CLIENT.
type ConnectedClient struct {
	Code          int32
	ServerID      uint32
	PlayerIDs     []uint32
	Session       Guid
	ShowID        string
	RemoteAddress string
	SessionID     string
	BuildInfo     string
	ServerTime    int64 // DateTime.ToBinary
}

func (c ConnectedClient) Encode() []byte {
	w := start(ServerConnectedClient)
	w.I32(c.Code)
	w.Packed(c.ServerID)
	w.Byte(byte(len(c.PlayerIDs)))
	for _, id := range c.PlayerIDs {
		w.Packed(id)
	}
	w.Bytes(c.Session[:])
	w.String(c.ShowID)
	w.String(c.RemoteAddress)
	w.Bool(false) // IsCrossPlatform: no value
	w.String(c.SessionID)
	w.String(c.BuildInfo)
	w.I64(c.ServerTime)
	return w.B
}

// DotNetTicks is DateTime.ToBinary for a UTC time given in Unix nanoseconds: ticks since
// 0001-01-01 with the UTC kind bit.
func DotNetTicks(unixNano int64) int64 {
	const epoch = 621355968000000000
	return (epoch + unixNano/100) | 1<<62
}

// ---------------------------------------------------------------------------------------------
// Round options and loading

// Options is the NetworkGameOptions subset a solo race sets; everything else goes out zero.
type Options struct {
	Duration              float32 // seconds
	RoundID               string  // a round id of the CMS document
	BucketID              string
	Seed                  int32
	LoadingScreen         string
	SceneName             string
	MinParticipants       uint32
	MaxParticipants       uint32
	CurrentParticipants   uint32
	QualificationPercent  uint32
	IsFinalRound          bool
	RoundIndex            uint32
	InitialRoundPlayers   uint32
	ShowResetToCheckpoint bool
}

func (o Options) Encode() []byte {
	w := start(ServerGameDataOptions)
	// GameDurationSettings
	w.F32(o.Duration)
	w.F32(o.Duration) // timer visibility
	w.F32(0)          // lobby timeout remaining
	w.String(o.RoundID)
	w.String(o.BucketID)
	w.I32(o.Seed) // director seed
	w.I32(o.Seed) // random seed
	w.Bool(false) // bots present
	w.Bool(false) // force variants with bots
	w.String(o.LoadingScreen)
	w.String(o.SceneName)
	w.F32(30) // player name max distance
	w.String("")
	w.Packed(o.MinParticipants)
	w.Packed(o.MaxParticipants)
	w.Packed(o.CurrentParticipants)
	w.Bool(false) // no switcher overrides
	w.String("")  // creator round override
	w.Packed(0)   // TeamMode
	w.Packed(0)   // TeamCount
	w.Bool(false) // UseTeamDistributionRatios
	w.Packed(0)   // TeamDistributionBackfilling
	w.Bool(false) // UseTeamDynamicScoreTargetMultipliers
	w.Packed(0)   // RequiredTeamEliminations
	w.Bool(false) // IsScoringGame
	w.Bool(false) // CheckSoloScoresAtSessionEnd
	w.Packed(0)   // ScoreTarget
	w.Bool(false) // IsScoreTargetDynamic
	w.Packed(0)   // ScoreTargetSquad2Players
	w.Packed(0)   // 3
	w.Packed(0)   // 4
	w.Packed(0)   // GameManagerSpawnType
	w.Packed(0)   // OvertimeMode
	w.F32(0)      // OvertimeAmount
	w.Packed(0)   // RoundEndCondition
	w.Packed(0)   // RoundEndLeniencyMs
	w.Bool(false) // UseTeamIdxWinsOnTimeElapsed
	w.Packed(0)   // TeamIdxWinsOnTimeElapsed
	w.Packed(o.QualificationPercent)
	w.Packed(0)   // SquadsQualificationPercentage
	w.Bool(false) // EndRoundIfEnoughTeamsQuit
	w.Bool(o.IsFinalRound)
	w.Packed(o.RoundIndex)
	w.Bool(false) // UseShouldHideOpposingTeamNameTags
	w.Bool(false) // ShowSoloLeaderboard
	w.Bool(false) // ShowSoloScoringIcon
	w.Packed(o.InitialRoundPlayers)
	w.Bool(o.ShowResetToCheckpoint)
	w.Bool(false) // IsPiggybackEnabled
	w.Bool(false) // BodyBlockingDisabled
	return w.B
}

// Empty is a message with no fields (START_LOADING_LEVEL).
func Empty(t Type) []byte { return start(t).B }

// Readiness states the client walks through (PlayerReadinessState).
const (
	ReadyNotReady       = 0
	ReadyLevelDetails   = 1
	ReadyLevelLoaded    = 2
	ReadyObjectsSpawned = 3
	ReadyIntroPlaying   = 4
	ReadyIntroComplete  = 5
	ReadyReadyToPlay    = 6
)

// SetReady is CLIENT_SET_READY.
type SetReady struct {
	NetworkID uint64
	State     byte
	Extra     string // ugc hash (state 2) or scene name (state 6)
}

func DecodeSetReady(r *hlapi.Reader) (SetReady, error) {
	s := SetReady{NetworkID: r.U64(), State: r.Byte()}
	if s.State == ReadyLevelLoaded || s.State == ReadyReadyToPlay {
		s.Extra = r.String()
	}
	return s, r.Err
}

// RequestSpawn is CLIENT_REQUEST_SPAWN_PLAYER.
type RequestSpawn struct {
	PlayerID   uint32
	Name       string
	LocalIndex byte
}

func DecodeRequestSpawn(r *hlapi.Reader) (RequestSpawn, error) {
	s := RequestSpawn{PlayerID: r.Packed(), Name: readShortString(r), LocalIndex: r.Byte()}
	return s, r.Err
}

// ---------------------------------------------------------------------------------------------
// Spawning

// Spawn object types.
const (
	SpawnPlayer = 0
	SpawnNPC    = 1
	SpawnObject = 2
)

// Customisation is CustomisationSelections: CRC32s of item ids, 0 for none.
type Customisation struct {
	Items        [9]uint32 // colour, pattern, top, bottom, full costume, faceplate, victory, nickname, nameplate
	EmoteWheel1  [8]uint32
	EmoteWheel2  [8]uint32
	Crowns, Fame int32
	FameUpdated  int64
}

// Player is the PlayerSpawnData of a fall guy.
type Player struct {
	OwnerNetworkID uint64
	PlayerID       uint32
	AccountID      string
	PlatformID     string
	PlatformName   string
	GeneratedName  string
	SquadID        uint32
	TeamID         int32
	PartyID        string
	VsGroupID      int32
	Customisation  Customisation
}

// Spawn is SERVER_SPAWN_OBJECT for a player.
type Spawn struct {
	IsAuth     bool // true on the copy for the owner: it drives the character
	NetID      uint32
	Type       byte
	PrefabHash int32
	Position   [3]float32
	Yaw        float32 // degrees about Y
	Player     Player
}

func (s Spawn) Encode() []byte {
	w := start(ServerSpawnObject)
	w.Bool(s.IsAuth)
	w.Packed(s.NetID)
	w.Byte(s.Type)
	w.I32(s.PrefabHash)
	Compact(w, s.Position)
	CompressedQuat(w, YawQuat(s.Yaw))
	Compact(w, [3]float32{1, 1, 1})
	w.Bool(true)  // sync transform
	w.Byte(0)     // creation mode: Spawn
	w.Byte(0)     // LOD controller behaviour
	w.Bool(false) // unified setup
	w.F32(0)      // self destruction time
	w.F32(0)      // self destruction timer start
	w.Packed(0)   // RMI identifier
	p := s.Player
	w.U64(p.OwnerNetworkID)
	w.Packed(p.PlayerID)
	w.String(p.AccountID)
	w.String(p.PlatformID)
	w.String(p.PlatformName)
	w.String(p.GeneratedName)
	w.Packed(p.SquadID)
	w.I32(p.TeamID)
	w.String(p.PartyID)
	w.I32(p.VsGroupID)
	w.Bool(false) // accessory enabled
	c := p.Customisation
	for _, v := range c.Items {
		w.U32(v)
	}
	for _, v := range c.EmoteWheel1 {
		w.U32(v)
	}
	for _, v := range c.EmoteWheel2 {
		w.U32(v)
	}
	w.I32(c.Crowns)
	w.I32(c.Fame)
	w.I64(c.FameUpdated)
	return w.B
}

// Unspawn is SERVER_UNSPAWN_OBJECT.
func Unspawn(netID uint32) []byte {
	w := start(ServerUnspawnObject)
	w.Packed(netID)
	return w.B
}

// Generic event types (EventGeneric).
const (
	EventAllPlayersSpawned   = 1
	EventForceDisconnect     = 2
	EventTimeSync            = 4
	EventQueuedObjectsSpawn  = 7
	EventRequiredQualifiers  = 8
	EventServerDisconnecting = 9
	EventServerEndingRound   = 10
	EventStartIntroCameras   = 14
)

// Payload is GameMessageGenericPayload; nil fields are left out of the mask.
type Payload struct {
	Int1, Int2     *uint32
	Float1, Float2 *float32
	Str1, Str2     *string
	NetID          *uint32
}

// Event is SERVER_EVENT_GENERIC.
func Event(eventType byte, p Payload) []byte {
	w := start(ServerEventGeneric)
	w.Byte(eventType)
	var mask byte
	for i, set := range []bool{p.Int1 != nil, p.Int2 != nil, p.Float1 != nil, p.Float2 != nil, p.Str1 != nil, p.Str2 != nil, p.NetID != nil} {
		if set {
			mask |= 1 << i
		}
	}
	w.Byte(mask)
	if p.Int1 != nil {
		w.Packed(*p.Int1)
	}
	if p.Int2 != nil {
		w.Packed(*p.Int2)
	}
	if p.Float1 != nil {
		w.F32(*p.Float1)
	}
	if p.Float2 != nil {
		w.F32(*p.Float2)
	}
	if p.Str1 != nil {
		ShortString(w, *p.Str1)
	}
	if p.Str2 != nil {
		ShortString(w, *p.Str2)
	}
	if p.NetID != nil {
		w.Packed(*p.NetID)
	}
	return w.B
}

// ---------------------------------------------------------------------------------------------
// Play

// StartGame is SERVER_START_GAME for a solo round: no teams, groups or entities.
func StartGame(startTime, endTime float32, participants byte, round Guid) []byte {
	w := start(ServerStartGame)
	w.F32(startTime)
	w.F32(endTime)
	w.Byte(0) // teams
	w.Byte(participants)
	w.Byte(0) // vs groups
	w.Byte(0) // entities
	w.Byte(0) // bots remaining
	w.Bytes(round[:])
	return w.B
}

// PingMsg is the client's PING.
type PingMsg struct {
	Data, Created float32
	Seq           uint32
	X             int32
}

func DecodePing(r *hlapi.Reader) (PingMsg, error) {
	p := PingMsg{Data: r.F32(), Created: r.F32(), Seq: r.U32(), X: r.I32()}
	return p, r.Err
}

// EncodePong answers a ping. now is the server's simulation clock in seconds; processing is
// how long the server held the ping.
func EncodePong(p PingMsg, processing, now float32) []byte {
	w := start(Pong)
	w.F32(processing)
	w.F32(p.Created)
	w.F32(p.Data)
	w.F32(now) // simulation time
	w.F32(now) // simulation fixed time
	return w.B
}

// Transform is SERVER_OBJECT_TRANSFORM, the uncompressed snapshot of one object.
type Transform struct {
	Time     float32
	NetID    uint32
	Flags    byte
	Position [3]float32
	Rotation [4]float32
	Velocity [3]float32
	AngVel   [3]float32
	Scale    [3]float32
}

func (t Transform) Encode() []byte {
	w := start(ServerObjectTransform)
	w.F32(t.Time)
	w.Packed(t.NetID)
	w.Byte(t.Flags)
	w.Vec3(t.Position)
	w.Quat(t.Rotation)
	w.Vec3(t.Velocity)
	w.Vec3(t.AngVel)
	w.Vec3(t.Scale)
	return w.B
}

// ---------------------------------------------------------------------------------------------
// Results

// Progress is SERVER_PLAYER_PROGRESS.
type Progress struct {
	PlayerID    uint32
	Succeeded   bool
	IsFinal     bool
	QualifyTime int32 // milliseconds
	Respawns    int32
}

func (p Progress) Encode() []byte {
	w := start(ServerPlayerProgress)
	w.U32(p.PlayerID)
	w.Bool(p.Succeeded)
	w.Bool(p.IsFinal)
	w.Byte(0) // cause: individual
	w.I16(0)  // carry type
	w.I16(0)  // piggyback position
	w.Bool(false)
	w.I32(p.Respawns)
	w.I32(p.QualifyTime)
	w.I32(0) // team goals
	w.I32(0) // hoops
	w.I32(0) // time survived
	w.I32(0) // manual restarts
	w.Bool(false)
	w.Bool(false)
	return w.B
}

// Qualification is SERVER_QUALIFICATION_PROGRESS_UPDATED.
func Qualification(qualified, eliminated uint32) []byte {
	w := start(ServerQualification)
	w.U32(qualified)
	w.U32(eliminated)
	return w.B
}

// Episode progress (EpisodeProgressStatus) and player states (PlayerProgressState).
const (
	EpisodeFail     = 0
	EpisodeNext     = 1
	EpisodeComplete = 2

	PlayerPlaying   = 1
	PlayerSucceeded = 2
	PlayerFailed    = 3
)

// PlayerState pairs a player with its round outcome.
type PlayerState struct {
	PlayerID uint32
	State    byte
}

// EndRound is SERVER_END_ROUND as one client sees it.
func EndRound(episode byte, players []PlayerState) []byte {
	w := start(ServerEndRound)
	w.Byte(episode)
	w.I32(int32(len(players)))
	for _, p := range players {
		w.Packed(p.PlayerID)
		w.Byte(p.State)
	}
	w.Packed(0)   // highest checkpoint
	w.Bool(false) // no new teams could qualify
	return w.B
}

// Result is one RoundResult.
type Result struct {
	NetID, PlayerID uint32
	Position        uint32
	Succeeded       bool
	Score           float32
}

// RoundResults is SERVER_ROUND_RESULTS.
func RoundResults(final, more bool, results []Result) []byte {
	w := start(ServerRoundResults)
	w.Bool(final)
	w.Bool(more)
	w.I32(int32(len(results)))
	for _, r := range results {
		w.Packed(r.NetID)
		w.Packed(r.PlayerID)
		w.Byte(0) // team id + 1: no team
		w.Packed(0)
		w.Packed(0)
		w.Packed(r.Position)
		w.Bool(r.Succeeded)
		w.String("")
		w.F32(r.Score)
	}
	return w.B
}

// ReadyRoundResponse answers CLIENT_READY_ROUND.
func ReadyRoundResponse(code byte, playerIDs []uint32) []byte {
	w := start(ServerReadyRoundResp)
	w.Byte(code)
	w.Byte(byte(len(playerIDs)))
	for _, id := range playerIDs {
		w.Packed(id)
	}
	return w.B
}

// ---------------------------------------------------------------------------------------------
// Compressions

// Compact writes a Vector3 as three IEEE half floats (WriteCompact).
func Compact(w *hlapi.Writer, v [3]float32) {
	for _, f := range v {
		w.U16(Half(f))
	}
}

// Half converts to IEEE 754 binary16, rounding to nearest.
func Half(f float32) uint16 {
	b := math.Float32bits(f)
	sign := uint16(b>>16) & 0x8000
	exp := int((b>>23)&0xff) - 127 + 15
	mant := b & 0x7fffff
	switch {
	case (b>>23)&0xff == 0xff: // inf/nan
		if mant != 0 {
			return sign | 0x7e00
		}
		return sign | 0x7c00
	case exp >= 31:
		return sign | 0x7c00
	case exp <= 0:
		if exp < -10 {
			return sign
		}
		mant |= 0x800000
		shift := uint(14 - exp)
		h := uint16(mant >> shift)
		if mant>>(shift-1)&1 != 0 {
			h++
		}
		return sign | h
	}
	h := sign | uint16(exp)<<10 | uint16(mant>>13)
	if mant&0x1000 != 0 {
		h++
	}
	return h
}

// YawQuat is a rotation of deg degrees about Y, as x, y, z, w.
func YawQuat(deg float32) [4]float32 {
	r := float64(deg) * math.Pi / 360
	return [4]float32{0, float32(math.Sin(r)), 0, float32(math.Cos(r))}
}

// CompressedQuat is WriteCompressedQuaternion: smallest three. The index of the largest
// component; OR 4 and nothing more when it is (almost) 1, else the other three as int16 times
// the sign of the largest.
func CompressedQuat(w *hlapi.Writer, q [4]float32) {
	big := 0
	for i := 1; i < 4; i++ {
		if math.Abs(float64(q[i])) > math.Abs(float64(q[big])) {
			big = i
		}
	}
	if math.Abs(float64(q[big])) > 0.9999 {
		w.Byte(byte(big) | 4)
		return
	}
	w.Byte(byte(big))
	sign := float32(1)
	if q[big] < 0 {
		sign = -1
	}
	for i := 0; i < 4; i++ {
		if i != big {
			w.I16(int16(q[i] * sign * 32767))
		}
	}
}
