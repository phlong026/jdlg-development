FROM node:22-alpine AS ui
WORKDIR /ui
COPY phase1/ui/package*.json ./
RUN npm ci
COPY phase1/ui/ ./
RUN npm run build

FROM maven:3.9-eclipse-temurin-17 AS build
WORKDIR /workspace
COPY src/main/java/ src/main/java/
COPY phase1/pom.xml phase1/pom.xml
COPY phase1/src/ phase1/src/
COPY --from=ui /ui/dist/ phase1/src/main/resources/static/
RUN --mount=type=cache,target=/root/.m2 mvn -f phase1/pom.xml -q -DskipTests package

FROM eclipse-temurin:17-jre-jammy
RUN groupadd --system crm && useradd --system --gid crm --home-dir /app crm
WORKDIR /app
COPY LICENSE /app/LICENSE
COPY phase1/UPSTREAM.md /app/UPSTREAM.md
COPY --from=build --chown=crm:crm /workspace/phase1/target/iyque-customer-phase1-1.0.0.jar app.jar
USER crm
EXPOSE 8080
ENTRYPOINT ["java","-XX:MaxRAMPercentage=75","-jar","/app/app.jar"]
