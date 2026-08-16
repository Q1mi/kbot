// Command worker 运行 KB 入库、审批续跑与分区维护任务。
// 分区维护可通过 --run-once 单独执行。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/hibiken/asynq"

	"github.com/Q1mi/kbot/internal/config"
	"github.com/Q1mi/kbot/internal/infrastructure/jobs"
	"github.com/Q1mi/kbot/internal/infrastructure/objstore"
	"github.com/Q1mi/kbot/internal/infrastructure/otel"
	"github.com/Q1mi/kbot/internal/infrastructure/postgres"
	redisinfra "github.com/Q1mi/kbot/internal/infrastructure/redis"
	larkintegration "github.com/Q1mi/kbot/internal/integration/lark"
	"github.com/Q1mi/kbot/internal/platform"
	"github.com/Q1mi/kbot/internal/platform/approval"
	"github.com/Q1mi/kbot/internal/platform/kb"
	"github.com/Q1mi/kbot/internal/platform/maintenance"
	"github.com/Q1mi/kbot/internal/platform/prompt"
	"github.com/Q1mi/kbot/internal/platform/tool"
	"github.com/Q1mi/kbot/internal/runtime/cache"
	"github.com/Q1mi/kbot/internal/runtime/engine"
	"github.com/Q1mi/kbot/internal/runtime/llm"
	"github.com/Q1mi/kbot/internal/runtime/retriever"
	"github.com/Q1mi/kbot/internal/runtime/sandbox"
)

// archiveBucket 是冷分区归档的对象存储桶。
const archiveBucket = "kbot-archive"

func main() {
	runOnce := flag.String("run-once", "", "monthly-partition(建分区)| archive-old-partitions(归档旧分区):跑一次后退出")
	flag.Parse()

	cfg := config.Load()
	cfg.MustValidate()
	ctx := context.Background()
	otelCleanup := otel.MustInit(ctx, otel.Config{
		Endpoint: cfg.OTLPEndpoint, Headers: cfg.OTLPHeaders,
		ServiceName: "kbot-worker", ServiceVersion: cfg.ServiceVersion,
		Environment: cfg.Environment, SampleRatio: cfg.OTELSampleRatio,
	})
	defer otelCleanup()

	db := postgres.MustOpen(ctx, cfg.DatabaseURL)
	defer db.Close()
	rds := redisinfra.MustOpen(ctx, cfg.RedisURL)
	defer rds.Close()

	embedder, err := retriever.NewEmbedder(cfg.EmbedderKind, cfg.EmbedderDim, cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.EmbedderModel)
	if err != nil {
		log.Fatalf("build embedder: %v", err)
	}
	sandboxClient, err := sandbox.NewClientWithTimeout(cfg.SandboxRunnerURL, cfg.SandboxRunnerToken, cfg.SandboxRunnerTimeout)
	if err != nil {
		log.Fatalf("build sandbox runner client: %v", err)
	}
	plat := platform.NewServiceWithSandboxRunner(db, rds, cfg.JWTKeyBytes(), prompt.NoopPublisher{}, embedder, nil,
		sandboxClient, []byte(cfg.CredentialEncryptionKey))
	endpointPolicy := tool.NewEndpointPolicy(cfg.ToolAllowedHosts, cfg.ToolAllowPrivateNetwork)
	plat.Tool.ConfigureEndpointPolicy(endpointPolicy)
	plat.ModelConfig.ConfigureEndpointPolicy(endpointPolicy)
	plat.KB.ConfigureMarkdownAllowedRoots(cfg.KBMarkdownAllowedRoots)
	defer plat.Audit.Close()

	// 归档对象存储（kbot-archive 桶）。不可用时关闭归档调度并保留数据库分区。
	var archive *objstore.Client
	if cfg.S3Endpoint != "" {
		archive, err = objstore.New(ctx, cfg.S3Endpoint, cfg.S3AccessKey, cfg.S3SecretKey, archiveBucket)
		if err != nil {
			log.Printf("objstore 不可用，分区归档已关闭: %v", err)
			archive = nil
		}
	}
	maint := maintenance.NewService(db, archive, cfg.AuditArchiveAfterMonths)

	// --run-once:手动跑一次后退出。
	if *runOnce != "" {
		switch *runOnce {
		case "monthly-partition", "ensure-partitions":
			if err := maint.EnsureUpcomingPartitions(ctx, time.Now().UTC()); err != nil {
				log.Fatalf("monthly-partition: %v", err)
			}
		case "archive-old-partitions", "archive-partitions":
			if _, err := maint.ArchiveOldPartitions(ctx, time.Now().UTC()); err != nil {
				log.Fatalf("archive-old-partitions: %v", err)
			}
		default:
			log.Fatalf("未知 --run-once %q(应为 monthly-partition|archive-old-partitions)", *runOnce)
		}
		return
	}

	// 常态启动:先确保分区存在(写入即落真实月分区,而非 default 兜底)。
	if err := maint.EnsureUpcomingPartitions(ctx, time.Now().UTC()); err != nil {
		log.Printf("启动期建分区失败(降级用 default 分区): %v", err)
	}

	// cron 调度：每月 25 日建下月分区；归档存储可用时每月 1 日归档旧分区。
	scheduler := jobs.NewScheduler(rds)
	if _, err := scheduler.Register("@every 15s", asynq.NewTask(jobs.TypeApprovalResumeDispatch, nil)); err != nil {
		log.Printf("register approval resume dispatch: %v", err)
	}
	if _, err := scheduler.Register("0 0 25 * *", asynq.NewTask(maintenance.TaskEnsurePartitions, nil)); err != nil {
		log.Printf("register ensure-partitions cron: %v", err)
	}
	if archive != nil {
		if _, err := scheduler.Register("0 0 1 * *", asynq.NewTask(maintenance.TaskArchivePartitions, nil)); err != nil {
			log.Printf("register archive-partitions cron: %v", err)
		}
	} else {
		log.Printf("archive-partitions cron disabled: object storage unavailable")
	}
	go func() {
		if err := scheduler.Run(); err != nil {
			log.Printf("scheduler stopped: %v", err)
		}
	}()

	// 审批通过后的会话由 worker 恢复执行。
	llmGateway, err := llm.NewGateway(cfg)
	if err != nil {
		log.Fatalf("build llm gateway: %v", err)
	}
	llmGateway.WithEndpointPolicy(endpointPolicy)
	llmGateway.WithProfileResolver(plat.ModelConfig)
	llmGateway.WithProjectQuota(plat.ModelConfig)
	llmGateway.WithCallSink(llm.NewPgModelCallSink(db))
	resumeEngine := engine.NewEngine(plat.Agent, llmGateway, plat.Registry).
		WithGuard(plat.Guard).WithAudit(plat.Audit).WithToolAudit(plat.Tool).WithApprovals(plat.ApprovalGate()).
		WithTracing(engine.TraceOptions{CaptureContent: cfg.OTELCaptureContent})

	server := jobs.NewServer(rds, 10)
	jobsClient := jobs.NewClient(rds)
	defer jobsClient.Close()
	server.HandleFunc(kb.TypeIngestDocument, plat.KB.HandleIngest())
	server.HandleFunc(jobs.TypeEngineResume, func(ctx context.Context, t *asynq.Task) error {
		var p jobs.ResumePayload
		if err := json.Unmarshal(t.Payload(), &p); err != nil {
			return err
		}
		if p.ConversationID == "" || p.ApprovalID == "" {
			return fmt.Errorf("engine_resume requires conversation_id and approval_id")
		}
		_, err := resumeEngine.Resume(ctx, p.ConversationID, p.ApprovalID)
		if errors.Is(err, approval.ErrExecutionUnavailable) {
			return nil
		}
		return err
	})
	server.HandleFunc(jobs.TypeApprovalResumeDispatch, func(ctx context.Context, _ *asynq.Task) error {
		ready, err := plat.Approvals.ListReadyResumes(ctx, 100)
		if err != nil {
			return err
		}
		for _, item := range ready {
			payload, err := json.Marshal(jobs.ResumePayload{ConversationID: item.ConversationID, ApprovalID: item.ID})
			if err != nil {
				return err
			}
			_, err = jobsClient.Enqueue(
				asynq.NewTask(jobs.TypeEngineResume, payload),
				asynq.TaskID("approval-resume-"+strings.ReplaceAll(item.ID, "-", "")),
			)
			if err != nil && !errors.Is(err, asynq.ErrTaskIDConflict) {
				return err
			}
		}
		return nil
	})
	larkOutbound := larkintegration.NewOutbound(cfg.LarkAppID, cfg.LarkAppSecret)
	server.HandleFunc(jobs.TypeLarkReply, larkintegration.ReplyTaskHandler(
		resumeEngine, larkOutbound, cache.NewRedisIdemStore(rds),
	))
	server.HandleFunc(maintenance.TaskEnsurePartitions, func(ctx context.Context, _ *asynq.Task) error {
		return maint.EnsureUpcomingPartitions(ctx, time.Now().UTC())
	})
	server.HandleFunc(maintenance.TaskArchivePartitions, func(ctx context.Context, _ *asynq.Task) error {
		_, err := maint.ArchiveOldPartitions(ctx, time.Now().UTC())
		return err
	})

	log.Println("kbot worker started, consuming async jobs + partition cron...")
	if err := server.Start(); err != nil {
		log.Fatalf("worker error: %v", err)
	}
}
