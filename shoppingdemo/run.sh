cd restserver
go build .
./demo &
cd ..
cd frontend
go build .
./client
