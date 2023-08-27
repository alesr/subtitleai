package subtitleai

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"

	pb "github.com/alesr/audiostripper/api/proto/audiostripper/v1"
	"github.com/alesr/bytesplitter"
	"github.com/alesr/whisperclient"
	"google.golang.org/grpc"
)

const videoChunkSize bytesplitter.MB = 3

type audiostripper interface {
	ExtractAudio(ctx context.Context, opts ...grpc.CallOption) (pb.AudioStripper_ExtractAudioClient, error)
}

type whisperClient interface {
	TranscribeAudio(ctx context.Context, in whisperclient.TranscribeAudioInput) ([]byte, error)
}

type Subtitler struct {
	audioStripper audiostripper
	whisperClient whisperClient
}

func New(audioStripper audiostripper, whisperClient whisperClient) *Subtitler {
	return &Subtitler{
		audioStripper: audioStripper,
		whisperClient: whisperClient,
	}
}

type Input struct {
	FileName   string
	SampleRate string
	Language   string // For now, we have the transcription language hardcoded to Portuguese.
	Data       []byte // 25MB limit for audio data. See: https://platform.openai.com/docs/guides/speech-to-text
}

// GenerateFromAudioData generates subtitle from audio data.
func (s *Subtitler) GenerateFromAudioData(ctx context.Context, in *Input) ([]byte, error) {
	audioData, err := s.extractAudio(ctx, in.Data, in.SampleRate)
	if err != nil {
		return nil, fmt.Errorf("could not extract audio: %w", err)
	}

	subtitleData, err := s.requestSubtitle(ctx, audioData, in.FileName, in.SampleRate)
	if err != nil {
		return nil, fmt.Errorf("could not generate subtitle: %w", err)
	}
	return subtitleData, nil
}

func (s *Subtitler) extractAudio(ctx context.Context, data []byte, sampleRate string) ([]byte, error) {
	videoChunks, err := bytesplitter.Split(data, videoChunkSize)
	if err != nil {
		return nil, fmt.Errorf("could not split data into chunks: %w", err)
	}

	stream, err := s.audioStripper.ExtractAudio(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not extract audio: %w", err)
	}

	// Sending sample rate separately since we only need to send it once.
	if err := stream.Send(&pb.VideoData{SampleRate: sampleRate}); err != nil {
		return nil, fmt.Errorf("could not send sample rate data: %w", err)
	}

	for _, chunck := range videoChunks {
		if err := stream.Send(&pb.VideoData{Data: chunck}); err != nil {
			return nil, fmt.Errorf("could not send video data: %w", err)
		}
	}

	if err := stream.CloseSend(); err != nil {
		log.Fatalf("Failed to close stream: %v", err)
	}

	var audioData []byte

	for {
		audioChunk, err := stream.Recv()
		if err == io.EOF {
			break
		}

		if err != nil {
			log.Fatalf("Failed to receive: %v", err)
		}

		audioData = append(audioData, audioChunk.Data...)
	}

	return audioData, nil
}

func (s *Subtitler) requestSubtitle(ctx context.Context, audioData []byte, fileName, sampleRate string) ([]byte, error) {
	subtitleData, err := s.whisperClient.TranscribeAudio(ctx, whisperclient.TranscribeAudioInput{
		Name:     fileName,
		Language: whisperclient.LanguagePortuguese, // TODO: extend support for other languages.
		Format:   whisperclient.FormatSrt,
		Data:     bytes.NewReader(audioData),
	})
	if err != nil {
		return nil, fmt.Errorf("could not generate subtitle: %w", err)
	}

	return subtitleData, nil
}
