package system

import (
	"log"
	"math"
	"strconv"
	"time"

	"github.com/EngoEngine/ecs"
	"github.com/drgomesp/midgarts/internal/component"
	"github.com/drgomesp/midgarts/internal/entity"
	"github.com/drgomesp/midgarts/internal/system/rendercmd"
	"github.com/drgomesp/midgarts/pkg/character"
	"github.com/drgomesp/midgarts/pkg/character/actionindex"
	"github.com/drgomesp/midgarts/pkg/character/actionplaymode"
	"github.com/drgomesp/midgarts/pkg/character/directiontype"
	"github.com/drgomesp/midgarts/pkg/fileformat/act"
	"github.com/drgomesp/midgarts/pkg/fileformat/grf"
	"github.com/drgomesp/midgarts/pkg/fileformat/spr"
	"github.com/drgomesp/midgarts/pkg/graphic"
	"github.com/go-gl/mathgl/mgl32"
)

const (
	SpriteScaleFactor    = float32(100.0)
	FixedCameraDirection = 6
)

type CharacterRenderable interface {
	component.CharacterStateComponentFace
	component.CharacterAttachmentComponentFace
}

type CharacterRenderSystem struct {
	grfFile         *grf.File
	characters      map[string]*entity.Character
	RenderCommands  *RenderCommands
	textureProvider graphic.TextureProvider
}

func NewCharacterRenderSystem(grfFile *grf.File, textureProvider graphic.TextureProvider) *CharacterRenderSystem {
	return &CharacterRenderSystem{
		grfFile:    grfFile,
		characters: map[string]*entity.Character{},
		RenderCommands: &RenderCommands{
			sprite: []rendercmd.SpriteRenderCommand{},
		},
		textureProvider: textureProvider,
	}
}

func (s *CharacterRenderSystem) Update(dt float32) {
	s.RenderCommands.sprite = []rendercmd.SpriteRenderCommand{}

	for _, char := range s.characters {
		s.renderCharacter(dt, char)
	}
}

func (s *CharacterRenderSystem) AddByInterface(o ecs.Identifier) {
	char := o.(*entity.Character)
	s.Add(char)
}

func (s *CharacterRenderSystem) Add(char *entity.Character) {
	cmp, e := component.NewCharacterAttachmentComponent(s.grfFile, component.CharacterAttachmentComponentConfig{
		Gender:           char.Gender,
		JobSpriteID:      char.JobSpriteID,
		HeadIndex:        char.HeadIndex,
		EnableShield:     char.HasShield,
		ShieldSpriteName: char.ShieldSpriteName,
	})
	if e != nil {
		log.Fatal(e)
	}

	char.SetCharacterAttachmentComponent(cmp)
	s.characters[strconv.Itoa(int(char.ID()))] = char
}

func (s *CharacterRenderSystem) Remove(e ecs.BasicEntity) {
	delete(s.characters, strconv.Itoa(int(e.ID())))
}

func (s *CharacterRenderSystem) renderCharacter(dt float32, char *entity.Character) {
	offset := [2]float32{0, 0}

	direction := int(char.Direction) + directiontype.DirectionTable[FixedCameraDirection]%8
	behind := direction > 1 && direction < 6
	renderShield := char.HasShield && char.ActionIndex == actionindex.StandBy

	if char.ActionIndex != actionindex.Dead && char.ActionIndex != actionindex.Sitting {
		s.renderAttachment(dt, char, character.AttachmentShadow, &offset)
	}

	if behind && renderShield {
		s.renderAttachment(dt, char, character.AttachmentShield, &offset)
	}

	s.renderAttachment(dt, char, character.AttachmentBody, &offset)
	s.renderAttachment(dt, char, character.AttachmentHead, &offset)

	if !behind && renderShield {
		s.renderAttachment(dt, char, character.AttachmentShield, &offset)
	}
}

func (s *CharacterRenderSystem) renderAttachment(
	dt float32,
	char *entity.Character,
	elem character.AttachmentType,
	offset *[2]float32,
) {

	var actions []*act.Action
	if actions = char.Files[elem].ACT.Actions; len(actions) == 0 {
		return
	}

	idx := (int(char.ActionIndex) + (int(char.Direction)+directiontype.DirectionTable[FixedCameraDirection])%8) % len(actions)
	action := actions[idx]
	frameCount := int64(len(action.Frames))
	timeNeededForOneFrame := int64(float64(action.Delay) * (1.0 / char.FPSMultiplier))

	if char.ForcedDuration != 0 {
		timeNeededForOneFrame = int64(char.ForcedDuration) / frameCount
	}

	timeNeededForOneFrame = int64(math.Max(float64(timeNeededForOneFrame), 100))
	elapsedTime := time.Since(char.AnimationStartedAt).Milliseconds() - int64(dt)
	realIndex := elapsedTime / timeNeededForOneFrame

	var frameIndex int64
	switch char.PlayMode {
	case actionplaymode.Repeat:
		frameIndex = realIndex % frameCount
		break
	}

	// Ignore "doridori" animation
	if len(action.Frames) == 3 {
		frameIndex = 0
	}

	var frame *act.ActionFrame
	if frame = action.Frames[frameIndex]; len(frame.Layers) == 0 {
		*offset = [2]float32{0, 0}
		return
	}

	position := [2]float32{0, 0}

	if len(frame.Positions) > 0 &&
		elem != character.AttachmentBody &&
		elem != character.AttachmentShield {
		position[0] = offset[0] - float32(frame.Positions[0][0])
		position[1] = offset[1] - float32(frame.Positions[0][1])
	}

	// Render all layers
	for _, layer := range frame.Layers {
		if layer.SpriteFrameIndex < 0 {
			continue
		}

		s.renderLayer(char, layer, char.Files[elem].SPR, position)
	}

	// Save offset reference
	if len(frame.Positions) > 0 {
		*offset = [2]float32{
			float32(frame.Positions[0][0]),
			float32(frame.Positions[0][1]),
		}
	}

	char.AnimationDelay = time.Duration(action.DurationMilliseconds) * time.Millisecond
}

func (s *CharacterRenderSystem) renderLayer(
	char *entity.Character,
	layer *act.ActionFrameLayer,
	spr *spr.SpriteFile,
	offset [2]float32,
) {
	frameIndex := int64(layer.SpriteFrameIndex)
	if frameIndex < 0 {
		return
	}

	texture, err := s.textureProvider.NewTextureFromRGBA(spr.ImageAt(int(frameIndex)))
	if err != nil {
		log.Fatal(err)
	}

	frame := spr.Frames[frameIndex]
	width, height := float32(frame.Width)*SpriteScaleFactor, float32(frame.Height)*SpriteScaleFactor
	width *= layer.Scale[0] * graphic.OnePixelSize
	height *= layer.Scale[1] * graphic.OnePixelSize
	rot := float64(layer.Angle) * (math.Pi / 180)

	offset = [2]float32{
		(float32(layer.Position[0]) + offset[0]) * graphic.OnePixelSize * SpriteScaleFactor,
		(float32(layer.Position[1]) + offset[1]) * graphic.OnePixelSize * SpriteScaleFactor,
	}

	// This is the current API to render a sprite. Commands will
	// be collected by the lower-level rendering system (OpenGL).
	s.renderSpriteCommand(rendercmd.SpriteRenderCommand{
		Scale:           layer.Scale,
		Size:            mgl32.Vec2{width, height},
		Position:        char.Position(),
		Offset:          mgl32.Vec2{offset[0], offset[1]},
		RotationRadians: float32(rot),
		Texture:         texture,
		FlipVertically:  layer.Mirrored,
	})
}

func (s *CharacterRenderSystem) renderSpriteCommand(cmd ...rendercmd.SpriteRenderCommand) {
	s.RenderCommands.sprite = append(s.RenderCommands.sprite, cmd...)
}
