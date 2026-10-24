package gamemsg

// Movement. Each console drives its own fall guy and sends CLIENT_MOTORTASKS: its inputs and
// where its character is. Other consoles see that player through SERVER_MOTORAGENT_STATE, whose
// transform uses the same TransformData encoding, so the server can pass the client's bytes on.
// (SERVER_OBJECT_TRANSFORM moves props only: the client ignores it for players.)

import (
	"errors"
	"math"

	"openpak/fall-guys/internal/hlapi"
)

// Snapshot is one entry of a CLIENT_MOTORTASKS.
type Snapshot struct {
	LocalFrame uint32
	ServerTick uint32
	Time       float32
	TaskMask   uint16
	Transform  TransformData
	// TransformBytes is the snapshot's TransformData as sent.
	TransformBytes []byte
}

// TransformData is a decoded transform: world position, euler angles in degrees, velocities.
type TransformData struct {
	Flags    byte
	Position [3]float32
	Euler    [3]float32
	Velocity [3]float32
	AngVel   [3]float32
}

// Bounds of TransformData's quantisation (its static constructor).
var (
	posMin = [3]float32{-256, -110, -320}
	posMax = [3]float32{256, 402, 320}
	rotMin = [3]float32{0, 0, 0}
	rotMax = [3]float32{360, 360, 360}
	velMin = [3]float32{-80, -80, -80}
	velMax = [3]float32{80, 80, 80}
	angMin = [3]float32{-15, -15, -15}
	angMax = [3]float32{15, 15, 15}
)

var bits32 = [3]uint32{2047, 1023, 2047} // x 11 bits, y 10, z 11

func dq(n uint32, lo, hi float32, steps uint32) float32 {
	return float32(n)/float32(steps)*(hi-lo) + lo
}

func quant(v, lo, hi float32, steps uint32) uint32 {
	t := (v - lo) / (hi - lo)
	t = max(0, min(t, 1))
	return uint32(math.RoundToEven(float64(t) * float64(steps)))
}

func readBounded48(r *hlapi.Reader, lo, hi [3]float32) (v [3]float32) {
	for i := range v {
		v[i] = dq(uint32(r.U16()), lo[i], hi[i], 65535)
	}
	return
}

func readBounded32(r *hlapi.Reader, lo, hi [3]float32) (v [3]float32) {
	u := r.U32()
	n := [3]uint32{u & 0x7ff, (u >> 11) & 0x3ff, (u >> 21) & 0x7ff}
	for i := range v {
		v[i] = dq(n[i], lo[i], hi[i], bits32[i])
	}
	return
}

func read24(r *hlapi.Reader, lo, hi float32) (float32, float32) {
	v := uint32(r.Byte()) | uint32(r.U16())<<8
	return dq(v&0xfff, lo, hi, 4095), dq(v>>12, lo, hi, 4095)
}

func axes(flags byte, shift uint) []int {
	var out []int
	for i := 0; i < 3; i++ {
		if flags>>(shift+uint(i))&1 != 0 {
			out = append(out, i)
		}
	}
	return out
}

// readAngles reads the one-, two- or three-axis form TransformData uses for rotation and
// angular velocity.
func readAngles(r *hlapi.Reader, ax []int, lo, hi [3]float32, out *[3]float32) {
	switch len(ax) {
	case 1:
		out[ax[0]] = dq(uint32(r.U16()), lo[ax[0]], hi[ax[0]], 65535)
	case 2:
		out[ax[0]], out[ax[1]] = read24(r, lo[ax[0]], hi[ax[0]])
	case 3:
		*out = readBounded32(r, lo, hi)
	}
}

// DecodeTransform reads a TransformData (flags parameter 0, as both snapshots write it).
func DecodeTransform(r *hlapi.Reader) TransformData {
	var t TransformData
	t.Flags = r.Byte()
	t.Position = readBounded48(r, posMin, posMax)
	readAngles(r, axes(t.Flags, 0), rotMin, rotMax, &t.Euler)
	if t.Flags&8 != 0 {
		if t.Flags&0x10 != 0 {
			t.Velocity = readBounded32(r, velMin, velMax)
		} else {
			t.Velocity[0], t.Velocity[2] = read24(r, -80, 80)
		}
	}
	readAngles(r, axes(t.Flags, 5), angMin, angMax, &t.AngVel)
	return t
}

// EncodeTransform writes a TransformData the way the client does (BuildBitFlags thresholds).
func EncodeTransform(w *hlapi.Writer, pos, euler, vel [3]float32) {
	var e [3]float32
	var f byte
	for i := range e {
		e[i] = float32(math.Mod(float64(euler[i]), 360))
		if e[i] < 0 {
			e[i] += 360
		}
		if e[i] >= 0.178 && e[i] <= 359.82 {
			f |= 1 << i
		}
	}
	if vel[0]*vel[0]+vel[1]*vel[1]+vel[2]*vel[2] >= 0.0064 {
		f |= 8
		if abs32(vel[1]) >= 0.08 {
			f |= 0x10
		}
	}
	w.Byte(f)
	for i := range pos {
		w.U16(uint16(quant(pos[i], posMin[i], posMax[i], 65535)))
	}
	switch ax := axes(f, 0); len(ax) {
	case 1:
		w.U16(uint16(quant(e[ax[0]], 0, 360, 65535)))
	case 2:
		write24(w, e[ax[0]], e[ax[1]], 0, 360)
	case 3:
		write32(w, e, rotMin, rotMax)
	}
	if f&8 != 0 {
		if f&0x10 != 0 {
			write32(w, vel, velMin, velMax)
		} else {
			write24(w, vel[0], vel[2], -80, 80)
		}
	}
}

func write24(w *hlapi.Writer, a, b, lo, hi float32) {
	v := quant(a, lo, hi, 4095) | quant(b, lo, hi, 4095)<<12
	w.Byte(byte(v))
	w.U16(uint16(v >> 8))
}

func write32(w *hlapi.Writer, v, lo, hi [3]float32) {
	var n [3]uint32
	for i := range n {
		n[i] = quant(v[i], lo[i], hi[i], bits32[i])
	}
	w.U32(n[0] | n[1]<<11 | n[2]<<21)
}

func abs32(f float32) float32 { return float32(math.Abs(float64(f))) }

// Motor task bits of a snapshot's mask (MotorTaskType).
const (
	TaskDive = iota
	TaskJump
	TaskMove
	TaskGrab
	TaskCarry
	TaskEmote
	TaskRagdoll
	TaskNoClip
	TaskPortal
	TaskEquipPowerup
	TaskActivatePowerup
	TaskSpeech
	TaskPiggyback
)

var errNoClip = errors.New("gamemsg: NoClip motor task on the wire")

func jointTarget(r *hlapi.Reader) (ok bool, id uint32) {
	ok = r.Byte() != 0
	if ok {
		id = r.Packed()
	}
	return
}

func skipTask(r *hlapi.Reader, task int) error {
	switch task {
	case TaskDive, TaskEmote, TaskRagdoll:
		r.Byte()
	case TaskJump:
	case TaskMove:
		r.Bytes(2)
	case TaskGrab:
		r.Byte()
		t := r.Byte()
		if ok, id := jointTarget(r); ok && id != 0 {
			r.Bytes(7)
			if t >= 2 && t <= 4 {
				r.Bytes(6)
			}
			if t == 4 {
				r.Byte()
			}
		}
	case TaskCarry:
		r.Packed()
	case TaskNoClip:
		return errNoClip
	case TaskPortal:
		jointTarget(r)
		jointTarget(r)
		r.U32()
		r.Bytes(6)
	case TaskEquipPowerup:
		jointTarget(r)
		r.F32()
	case TaskActivatePowerup:
		r.F32()
		r.U32()
	case TaskSpeech:
		r.Byte()
		r.F32()
	case TaskPiggyback:
		t := r.Byte()
		if t == 1 || t == 2 {
			r.Packed()
		}
		if t == 2 {
			r.Packed()
		}
	}
	return nil
}

func skipActivations(r *hlapi.Reader) {
	n := int(r.Byte())
	for i := 0; i < n && r.Err == nil; i++ {
		jointTarget(r)
		r.U16()
		if r.Byte()&1 != 0 {
			r.I16()
		}
	}
}

// DecodeMotorTasks reads a CLIENT_MOTORTASKS body: packed net id, 1-10 snapshots.
func DecodeMotorTasks(r *hlapi.Reader) (netID uint32, snaps []Snapshot, err error) {
	netID = r.Packed()
	n := int(r.Byte())
	if n < 1 || n > 10 {
		return netID, nil, errors.New("gamemsg: motor tasks: bad snapshot count")
	}
	for i := 0; i < n; i++ {
		s := Snapshot{LocalFrame: r.Packed(), ServerTick: r.Packed(), Time: r.F32()}
		r.Byte() // triggered character state
		r.Bool() // force apply transform
		r.Vec3() // camera direction
		s.TaskMask = r.U16()
		for task := 0; task < 13; task++ {
			if s.TaskMask>>task&1 != 0 {
				if err := skipTask(r, task); err != nil {
					return netID, snaps, err
				}
			}
		}
		at := r.Pos
		s.Transform = DecodeTransform(r)
		if r.Err == nil {
			s.TransformBytes = append([]byte(nil), r.B[at:r.Pos]...)
		}
		skipActivations(r)
		if r.Err != nil {
			return netID, snaps, r.Err
		}
		snaps = append(snaps, s)
	}
	return netID, snaps, nil
}

// AgentState is SERVER_MOTORAGENT_STATE for one player: one snapshot, no motor-function
// states, on the ground, the transform as the owner sent it.
func AgentState(netID, frame, tick uint32, now float32, transform []byte, grounded bool) []byte {
	w := start(ServerMotorAgentState)
	w.Packed(netID)
	w.Byte(1)
	w.Packed(frame)
	w.Packed(tick)
	w.F32(now)
	w.Byte(0) // motor function states
	if grounded {
		w.U16(0x20)
	} else {
		w.U16(0)
	}
	w.Byte(0) // transform flags
	w.Bytes(transform)
	w.Byte(128) // animation velocities, centred
	w.Byte(128)
	return w.B
}
