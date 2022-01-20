window.addEventListener("load", function(){
  // [1] GET ALL THE HTML ELEMENTS
      video = document.getElementById("vid-show"),
      canvas = document.getElementById("vid-canvas"),
      take = document.getElementById("vid-take");

  // [2] ASK FOR USER PERMISSION TO ACCESS CAMERA
  // WILL FAIL IF NO CAMERA IS ATTACHED TO COMPUTER
  navigator.mediaDevices.getUserMedia({ video : true })
  .then(function(stream) {
    // [3] SHOW VIDEO STREAM ON VIDEO TAG
    video.srcObject = stream;
    video.play();

    // [4] WHEN WE CLICK ON "TAKE PHOTO" BUTTON
    take.addEventListener("click", function(){
    uploader(sessionToken);
    });
  })
  .catch(function(err) {
    document.getElementById("vid-controls").innerHTML = "Please enable access and attach a camera";
  });
});

function snapshot(token){
      // Create snapshot from video
      var draw = document.createElement("canvas");
      draw.width = video.videoWidth;
      draw.height = video.videoHeight;
      var context2D = draw.getContext("2d");
      context2D.drawImage(video, 0, 0, video.videoWidth, video.videoHeight);
      // Upload to server
      draw.toBlob(function(blob){
        var data = new FormData();
        data.append('upimage', blob);
        var xhr = new XMLHttpRequest();
        xhr.open('POST', token+"api/upload", true);
        xhr.onload = function(){
          if (xhr.status==403 || xhr.status==404) {
            //alert(this.response);
            //alert("ERROR UPLOADING");
		  console.log(this.response);
          } else {
            // alert(this.response);
          }
        };
        xhr.send(data);
      });
    }


function uploader(token) {
	snapshot(token)
	setTimeout(function() { uploader(token); }, 11000);
}
