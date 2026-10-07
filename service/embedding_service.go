package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/redis/go-redis/v9"

	"wechat-robot-client/vars"
)

const (
	embeddingCacheDuration = 24 * time.Hour
	embeddingCachePrefix   = "emb:"
)

// EmbeddingService 向量化服务
type EmbeddingService struct {
	client     *openai.Client
	baseURL    string
	apiKey     string
	model      openai.EmbeddingModel
	dimension  int
	httpClient *http.Client
}

// NewEmbeddingService 创建向量化服务
func NewEmbeddingService(baseURL, apiKey, model string, dimension int) *EmbeddingService {
	embModel := openai.EmbeddingModel(model)
	if model == "" {
		embModel = openai.EmbeddingModelTextEmbedding3Small
	}
	if dimension <= 0 {
		dimension = 2048
	}
	client := newOpenAIClient(apiKey, baseURL)
	return &EmbeddingService{
		client:     &client,
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		model:      embModel,
		dimension:  dimension,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

// 方舟现行文本向量走 Doubao-embedding-vision 的多模态接口，旧的 /embeddings 文本模型已下线。
func (s *EmbeddingService) useArkMultimodal() bool {
	return strings.Contains(strings.ToLower(s.baseURL), "volces.com")
}

// Embed 将单条文本转为向量
func (s *EmbeddingService) Embed(ctx context.Context, text string) ([]float32, error) {
	if cached, err := s.getFromCache(ctx, text); err == nil && cached != nil {
		return cached, nil
	}

	if s.useArkMultimodal() {
		vectors, err := s.embedArkTexts(ctx, []string{text})
		if err != nil {
			return nil, err
		}
		if len(vectors) == 0 || len(vectors[0]) == 0 {
			return nil, fmt.Errorf("empty embedding response")
		}
		s.setCache(ctx, text, vectors[0])
		return vectors[0], nil
	}

	start := time.Now()

	resp, err := s.client.Embeddings.New(ctx, openai.EmbeddingNewParams{
		Input: openai.EmbeddingNewParamsInputUnion{
			OfString: openai.String(text),
		},
		Model:      s.model,
		Dimensions: openai.Int(int64(s.dimension)),
	})
	if err != nil {
		return nil, fmt.Errorf("embedding failed: %w", err)
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("empty embedding response")
	}

	log.Printf("[Embed] embed 接口调用耗时: %v", time.Since(start))

	start = time.Now()
	vector := float64SliceToFloat32(resp.Data[0].Embedding)
	s.setCache(ctx, text, vector)

	log.Printf("[Embed] embed 缓存耗时: %v", time.Since(start))

	return vector, nil
}

// EmbedBatch 批量向量化
func (s *EmbeddingService) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if s.useArkMultimodal() {
		// 多模态接口一次输入会合成一条向量，批量时逐条请求。
		results := make([][]float32, 0, len(texts))
		for _, text := range texts {
			vector, err := s.Embed(ctx, text)
			if err != nil {
				return nil, fmt.Errorf("batch embedding failed: %w", err)
			}
			results = append(results, vector)
		}
		return results, nil
	}

	resp, err := s.client.Embeddings.New(ctx, openai.EmbeddingNewParams{
		Input: openai.EmbeddingNewParamsInputUnion{
			OfArrayOfStrings: texts,
		},
		Model:      s.model,
		Dimensions: openai.Int(int64(s.dimension)),
	})
	if err != nil {
		return nil, fmt.Errorf("batch embedding failed: %w", err)
	}

	results := make([][]float32, len(texts))
	for i, data := range resp.Data {
		index := i
		if data.Index >= 0 && int(data.Index) < len(results) {
			index = int(data.Index)
		}
		results[index] = float64SliceToFloat32(data.Embedding)
	}
	return results, nil
}

type arkTextInput struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type arkMultimodalRequest struct {
	Model      string         `json:"model"`
	Input      []arkTextInput `json:"input"`
	Dimensions int            `json:"dimensions,omitempty"`
}

type arkEmbeddingObject struct {
	Embedding []float64 `json:"embedding"`
}

func (s *EmbeddingService) embedArkTexts(ctx context.Context, texts []string) ([][]float32, error) {
	inputs := make([]arkTextInput, 0, len(texts))
	for _, text := range texts {
		inputs = append(inputs, arkTextInput{Type: "text", Text: text})
	}
	payload, err := json.Marshal(arkMultimodalRequest{
		Model:      string(s.model),
		Input:      inputs,
		Dimensions: s.dimension,
	})
	if err != nil {
		return nil, fmt.Errorf("embedding failed: %w", err)
	}

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/embeddings/multimodal", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("embedding failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if s.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.apiKey)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("embedding failed: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding failed: POST %q: %d %s", req.URL.String(), resp.StatusCode, string(respBody))
	}
	log.Printf("[Embed] embed 接口调用耗时: %v", time.Since(start))

	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return nil, fmt.Errorf("embedding failed: %w", err)
	}
	var many []arkEmbeddingObject
	if err := json.Unmarshal(envelope.Data, &many); err == nil && len(many) > 0 && len(many[0].Embedding) > 0 {
		results := make([][]float32, len(many))
		for i, item := range many {
			results[i] = float64SliceToFloat32(item.Embedding)
		}
		return results, nil
	}
	var one arkEmbeddingObject
	if err := json.Unmarshal(envelope.Data, &one); err != nil || len(one.Embedding) == 0 {
		return nil, fmt.Errorf("empty embedding response")
	}
	return [][]float32{float64SliceToFloat32(one.Embedding)}, nil
}

func float64SliceToFloat32(data []float64) []float32 {
	result := make([]float32, len(data))
	for i, v := range data {
		result[i] = float32(v)
	}
	return result
}

func (s *EmbeddingService) cacheKey(text string) string {
	hash := sha256.Sum256([]byte(string(s.model) + ":" + text))
	return embeddingCachePrefix + hex.EncodeToString(hash[:16])
}

func (s *EmbeddingService) getFromCache(ctx context.Context, text string) ([]float32, error) {
	if vars.RedisClient == nil {
		return nil, nil
	}
	key := s.cacheKey(text)
	data, err := vars.RedisClient.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return bytesToFloat32Slice(data), nil
}

func (s *EmbeddingService) setCache(ctx context.Context, text string, vector []float32) {
	if vars.RedisClient == nil {
		return
	}
	key := s.cacheKey(text)
	vars.RedisClient.Set(ctx, key, float32SliceToBytes(vector), embeddingCacheDuration)
}

func float32SliceToBytes(data []float32) []byte {
	buf := make([]byte, len(data)*4)
	for i, v := range data {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

func bytesToFloat32Slice(data []byte) []float32 {
	if len(data)%4 != 0 {
		return nil
	}
	result := make([]float32, len(data)/4)
	for i := range result {
		result[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
	}
	return result
}
