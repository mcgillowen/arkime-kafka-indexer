FROM --platform=$BUILDPLATFORM gcr.io/distroless/base-debian12:latest
ENTRYPOINT [ "/arkime-kafka-indexer" ]
COPY arkime-kafka-indexer /arkime-kafka-indexer
