FROM alpine
RUN apk add --no-cache bash git nodejs npm
WORKDIR /app
COPY *.json ./
RUN npm install
COPY app.sh ./
CMD [ "bash", "app.sh" ]
EXPOSE 8080
