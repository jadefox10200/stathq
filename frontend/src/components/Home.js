export default function Home() {
  return (
    <div className="ui container" id="homeBody">
      <h1 className="center-it">Welcome to Firesmith Stats</h1>
      <video width="560" height="315" controls className="mx-auto">
        <source src="/public/AV/Instruction_Video_YT.mov" type="video/mp4" />
        Your browser does not support the video tag.
      </video>
      <label className="block text-center mt-2.5">
        Please select an option from the navigation bar above.
      </label>
    </div>
  );
}
