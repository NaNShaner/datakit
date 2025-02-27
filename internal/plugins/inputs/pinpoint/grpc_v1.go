// Unless explicitly stated otherwise all files in this repository are licensed
// under the MIT License.
// This product includes software developed at Guance Cloud (https://www.guance.com/).
// Copyright 2021-present Guance, Inc.

// Package pinpoint handle Pinpoint APM traces.
package pinpoint

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"

	ppv1 "github.com/GuanceCloud/tracing-protos/pinpoint-gen-go/v1"
	istorage "gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/storage"
	itrace "gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
	emptypb "google.golang.org/protobuf/types/known/emptypb"
)

func runGRPCV1(addr string) {
	if localCache != nil {
		localCache.RegisterConsumer(istorage.PINPOINT_GRPC_KEY, func(buf []byte) error {
			spanw := &ppv1.PSpanMessageWithMeta{}
			if err := proto.Unmarshal(buf, spanw); err != nil {
				return err
			}

			dktrace, err := parsePPSpanMessage(makeMeta(spanw.Meta), spanw.SpanMessage)
			if err != nil {
				return err
			}
			if spanSender != nil {
				spanSender.Append(dktrace...)
			}

			return nil
		})
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Errorf("### grpc server v1 listening on %s failed: %s", addr, err.Error())

		return
	}
	log.Debugf("### grpc server v1 listening on: %s", addr)

	gsvr = grpc.NewServer()
	ppv1.RegisterAgentServer(gsvr, &AgentServer{})
	ppv1.RegisterMetadataServer(gsvr, &MetadataServer{})
	ppv1.RegisterProfilerCommandServiceServer(gsvr, &ProfilerCommandServiceServer{})
	ppv1.RegisterStatServer(gsvr, &StatServer{})
	ppv1.RegisterSpanServer(gsvr, &SpanServer{})

	if err = gsvr.Serve(listener); err != nil {
		log.Error(err.Error())
	}
	log.Debug("### grpc server v1 exits")
}

type ServiceInfo struct {
	Name string
	Libs []string
}

type AgentMetaData struct {
	Hostname     string
	IP           string
	Ports        string
	ServiceType  int32
	Pid          int32
	AgentVersion string
	VMVersion    string
	EndTimestamp int64
	EndStatus    int32
	Container    bool
	ServerInfo   string
	VMArg        []string
	ServiceInfo  []*ServiceInfo
	Version      int32
	GcType       ppv1.PJvmGcType
}

type AgentServer struct {
	ppv1.UnimplementedAgentServer
}

func (agtsvr *AgentServer) RequestAgentInfo(ctx context.Context, agInfo *ppv1.PAgentInfo) (*ppv1.PResult, error) {
	agentMetaData.Hostname = agInfo.Hostname
	agentMetaData.IP = agInfo.Ip
	agentMetaData.Ports = agInfo.Ports
	agentMetaData.ServiceType = agInfo.ServiceType
	agentMetaData.Pid = agInfo.Pid
	agentMetaData.AgentVersion = agInfo.AgentVersion
	agentMetaData.VMVersion = agInfo.VmVersion
	agentMetaData.EndTimestamp = agInfo.EndTimestamp
	agentMetaData.EndStatus = agInfo.EndStatus
	agentMetaData.Container = agInfo.Container
	agentMetaData.ServerInfo = agInfo.ServerMetaData.ServerInfo
	agentMetaData.VMArg = agInfo.ServerMetaData.VmArg
	agentMetaData.Version = agInfo.JvmInfo.Version
	agentMetaData.GcType = agInfo.JvmInfo.GcType
	for _, v := range agInfo.ServerMetaData.ServiceInfo {
		agentMetaData.ServiceInfo = append(agentMetaData.ServiceInfo, &ServiceInfo{
			Name: v.ServiceName,
			Libs: v.ServiceLib,
		})
	}

	log.Debugf("### agent meta: %s", agInfo.String())

	return &ppv1.PResult{Success: true, Message: "ok"}, nil
}

func (agtsvr *AgentServer) PingSession(ping ppv1.Agent_PingSessionServer) error {
	msg, err := ping.Recv()
	if err != nil {
		log.Error(err.Error())

		return err
	}

	return ping.SendMsg(msg)
}

type reqtype string

const (
	reqsql reqtype = "sql-request"
	reqapi reqtype = "sql-api"
	reqstr reqtype = "sql-string"
)

func concatReqID(req reqtype, id int32) string {
	return fmt.Sprintf("%d:%s", id, req)
}

func newMetaData(id int32, meta interface{}) *requestMetaData {
	return &requestMetaData{ID: id, Meta: meta}
}

type MetadataServer struct {
	ppv1.UnimplementedMetadataServer
}

func (mdsvr *MetadataServer) RequestSqlMetaData(ctx context.Context, meta *ppv1.PSqlMetaData) (*ppv1.PResult, error) { // nolint: stylecheck
	if reqMetaTab != nil && meta != nil {
		reqMetaTab.Store(concatReqID(reqsql, meta.SqlId), newMetaData(meta.SqlId, meta))
	}

	return &ppv1.PResult{Success: true, Message: "ok"}, nil
}

func (mdsvr *MetadataServer) RequestApiMetaData(ctx context.Context, meta *ppv1.PApiMetaData) (*ppv1.PResult, error) { // nolint: stylecheck
	if reqMetaTab != nil && meta != nil {
		reqMetaTab.Store(concatReqID(reqapi, meta.ApiId), newMetaData(meta.ApiId, meta))
	}

	return &ppv1.PResult{Success: true, Message: "ok"}, nil
}

func (mdsvr *MetadataServer) RequestStringMetaData(ctx context.Context, meta *ppv1.PStringMetaData) (*ppv1.PResult, error) {
	if reqMetaTab != nil && meta != nil {
		reqMetaTab.Store(concatReqID(reqstr, meta.StringId), newMetaData(meta.StringId, meta))
	}

	return &ppv1.PResult{Success: true, Message: "ok"}, nil
}

type ProfilerCommandServiceServer struct {
	ppv1.UnimplementedProfilerCommandServiceServer
}

func (*ProfilerCommandServiceServer) HandleCommand(handler ppv1.ProfilerCommandService_HandleCommandServer) error {
	if _, err := handler.Recv(); err != nil {
		log.Error(err.Error())

		return err
	}

	return nil
}

func (*ProfilerCommandServiceServer) HandleCommandV2(handler ppv1.ProfilerCommandService_HandleCommandV2Server) error {
	msg, err := handler.Recv()
	if err != nil {
		log.Error(err.Error())

		return err
	}
	log.Debugf("### profiler handle command v2 %#v", msg)

	return nil
}

func (*ProfilerCommandServiceServer) CommandEcho(ctx context.Context, resp *ppv1.PCmdEchoResponse) (*emptypb.Empty, error) {
	log.Debugf("### profiler echo command %#v", resp)

	return &emptypb.Empty{}, nil
}

func (*ProfilerCommandServiceServer) CommandStreamActiveThreadCount(stream ppv1.ProfilerCommandService_CommandStreamActiveThreadCountServer) error {
	for {
		resp, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return stream.SendAndClose(&emptypb.Empty{})
			}
			log.Error(err.Error())

			return err
		}

		log.Debugf("### profiler stream active thread count command %#v", resp)
	}
}

func (*ProfilerCommandServiceServer) CommandActiveThreadDump(ctx context.Context, resp *ppv1.PCmdActiveThreadDumpRes) (
	*emptypb.Empty, error,
) {
	log.Debugf("### profiler active thread dump command %#v", resp)

	return &emptypb.Empty{}, nil
}

func (pcss *ProfilerCommandServiceServer) CommandActiveThreadLightDump(ctx context.Context,
	resp *ppv1.PCmdActiveThreadLightDumpRes,
) (*emptypb.Empty, error) {
	log.Debugf("### profiler active thread light dump command %#v", resp)

	return &emptypb.Empty{}, nil
}

type StatServer struct {
	ppv1.UnimplementedStatServer
}

func (*StatServer) SendAgentStat(statSvr ppv1.Stat_SendAgentStatServer) error {
	for {
		msg, err := statSvr.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return statSvr.SendAndClose(&emptypb.Empty{})
			}
			log.Debug(err.Error())

			return err
		}

		log.Debugf("### stat: %s", msg.String())
	}
}

type SpanServer struct {
	ppv1.UnimplementedSpanServer
}

func (*SpanServer) SendSpan(spanSvr ppv1.Span_SendSpanServer) error {
	for {
		md, _ := metadata.FromIncomingContext(spanSvr.Context())

		msg, err := spanSvr.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return spanSvr.SendAndClose(&emptypb.Empty{})
			}
			log.Debug(err.Error())

			return err
		}

		if localCache == nil || !localCache.Enabled() {
			dktrace, err := parsePPSpanMessage(md, msg)
			if err != nil {
				log.Debug(err.Error())
				continue
			}
			if spanSender != nil {
				log.Debugf("### span: %#v", dktrace)
				spanSender.Append(dktrace...)
			}
		} else {
			spanw := &ppv1.PSpanMessageWithMeta{
				SpanMessage: msg,
				Meta:        makePSpanMeta(md),
			}
			buf, err := proto.Marshal(spanw)
			if err != nil {
				log.Error(err.Error())
				continue
			}

			if err = localCache.Put(istorage.PINPOINT_GRPC_KEY, buf); err != nil {
				log.Error(err.Error())
			}
		}
	}
}

func parsePPSpanMessage(meta metadata.MD, msg *ppv1.PSpanMessage) (itrace.DatakitTrace, error) {
	var trace itrace.DatakitTrace
	if ppspan := msg.GetSpan(); ppspan != nil {
		trace = ConvertPSpanToDKTrace(inputName, ppspan, meta, reqMetaTab)
	} else if ppchunk := msg.GetSpanChunk(); ppchunk != nil {
		trace = ConvertPSpanChunkToDKTrace(inputName, ppchunk, meta, reqMetaTab)
	} else {
		return nil, errors.New("### empty span message")
	}

	// add on global tags
	if len(tags) != 0 {
		for _, span := range trace {
			if span.Tags == nil {
				span.Tags = make(map[string]string)
			}
			for k, v := range tags {
				span.Tags[k] = v
			}
		}
	}

	return trace, nil
}

func makePSpanMeta(md metadata.MD) map[string]*ppv1.StringSlice {
	if len(md) == 0 {
		return nil
	}

	spmd := make(map[string]*ppv1.StringSlice)
	for k, v := range md {
		spmd[k] = &ppv1.StringSlice{Values: v}
	}

	return spmd
}

func makeMeta(spmd map[string]*ppv1.StringSlice) metadata.MD {
	if len(spmd) == 0 {
		return nil
	}

	md := make(metadata.MD)
	for k, v := range spmd {
		md[k] = v.Values
	}

	return md
}
